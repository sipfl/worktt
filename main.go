// worktt — derive working hours from macOS knowledgeC.db (display backlight).
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// seconds between unix epoch (1970) and cocoa/mac absolute epoch (2001)
const cocoaEpoch = 978307200

// sub-minute backlight flicker is merged into the surrounding active block;
// gaps at or above this threshold count as breaks.
const mergeGap = 60 * time.Second

// isolated active blocks shorter than this (e.g. briefly waking the screen
// in the evening) are not real working time and get dropped.
const minActive = 90 * time.Second

type rawRow struct {
	start, end time.Time
	val        int
}

type interval struct{ start, end time.Time }

type dayStats struct {
	date    time.Time
	ivs     []interval
	hasData bool
}

func (d dayStats) begin() time.Time { return d.ivs[0].start }
func (d dayStats) end() time.Time   { return d.ivs[len(d.ivs)-1].end }
func (d dayStats) gross() time.Duration {
	return d.end().Sub(d.begin())
}
func (d dayStats) active() time.Duration {
	var sum time.Duration
	for _, iv := range d.ivs {
		sum += iv.end.Sub(iv.start)
	}
	return sum
}
func (d dayStats) breaks() time.Duration { return d.gross() - d.active() }

var deDay = map[time.Weekday]string{
	time.Monday: "Mo", time.Tuesday: "Di", time.Wednesday: "Mi",
	time.Thursday: "Do", time.Friday: "Fr", time.Saturday: "Sa", time.Sunday: "So",
}

func defaultDB() string {
	return filepath.Join(os.Getenv("HOME"), "Library/Application Support/Knowledge/knowledgeC.db")
}

func queryRows(db string, from, to time.Time) ([]rawRow, error) {
	uri := "file:" + db + "?immutable=1"
	q := fmt.Sprintf(`SELECT ZSTARTDATE, ZENDDATE, ZVALUEINTEGER FROM ZOBJECT `+
		`WHERE ZSTREAMNAME='/display/isBacklit' AND ZSTARTDATE >= %d AND ZSTARTDATE < %d `+
		`ORDER BY ZSTARTDATE;`, from.Unix()-cocoaEpoch, to.Unix()-cocoaEpoch)
	out, err := exec.Command("sqlite3", "-separator", "|", uri, q).Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3: %w", err)
	}
	var rows []rawRow
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) != 3 {
			continue
		}
		s, _ := strconv.ParseFloat(f[0], 64)
		e, _ := strconv.ParseFloat(f[1], 64)
		v, _ := strconv.Atoi(f[2])
		rows = append(rows, rawRow{
			start: time.Unix(int64(s)+cocoaEpoch, 0),
			end:   time.Unix(int64(e)+cocoaEpoch, 0),
			val:   v,
		})
	}
	return rows, nil
}

// mergeOn keeps only backlit-on intervals, drops zero-length flicker and
// merges intervals separated by less than mergeGap.
func mergeOn(rows []rawRow) []interval {
	var ons []interval
	for _, r := range rows {
		if r.val == 1 && r.end.After(r.start) {
			ons = append(ons, interval{r.start, r.end})
		}
	}
	sort.Slice(ons, func(i, j int) bool { return ons[i].start.Before(ons[j].start) })
	var m []interval
	for _, iv := range ons {
		if len(m) == 0 {
			m = append(m, iv)
			continue
		}
		last := &m[len(m)-1]
		if iv.start.Sub(last.end) < mergeGap {
			if iv.end.After(last.end) {
				last.end = iv.end
			}
		} else {
			m = append(m, iv)
		}
	}
	var out []interval
	for _, iv := range m {
		if iv.end.Sub(iv.start) >= minActive {
			out = append(out, iv)
		}
	}
	return out
}

func statsForDay(db string, day time.Time) (dayStats, error) {
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	to := from.AddDate(0, 0, 1)
	rows, err := queryRows(db, from, to)
	if err != nil {
		return dayStats{}, err
	}
	ivs := mergeOn(rows)
	return dayStats{date: from, ivs: ivs, hasData: len(ivs) > 0}, nil
}

func fmtDur(d time.Duration) string {
	m := int(d.Minutes() + 0.5)
	h := m / 60
	m = m % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func printDay(d dayStats) {
	hdr := fmt.Sprintf("%s, %s", deDay[d.date.Weekday()], d.date.Format("02.01.2006"))
	fmt.Println(hdr)
	fmt.Println(strings.Repeat("─", len(hdr)+8))
	if !d.hasData {
		fmt.Println("keine Aktivität")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "Von\tBis\tDauer\tStatus")
	for i, iv := range d.ivs {
		fmt.Fprintf(w, "%s\t%s\t%s\taktiv\n",
			iv.start.Format("15:04"), iv.end.Format("15:04"), fmtDur(iv.end.Sub(iv.start)))
		if i < len(d.ivs)-1 {
			next := d.ivs[i+1].start
			fmt.Fprintf(w, "%s\t%s\t%s\tPause\n",
				iv.end.Format("15:04"), next.Format("15:04"), fmtDur(next.Sub(iv.end)))
		}
	}
	w.Flush()
	fmt.Println()
	fmt.Printf("Beginn:  %s\n", d.begin().Format("15:04:05"))
	fmt.Printf("Ende:    %s\n", d.end().Format("15:04:05"))
	fmt.Printf("Brutto:  %s\n", fmtDur(d.gross()))
	fmt.Printf("Aktiv:   %s\n", fmtDur(d.active()))
	fmt.Printf("Pause:   %s\n", fmtDur(d.breaks()))
}

func printRange(db string, end time.Time) error {
	start := end.AddDate(0, 0, -6)
	fmt.Printf("Letzte 7 Tage (%s – %s)\n\n", start.Format("02.01.2006"), end.Format("02.01.2006"))
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "Tag\tDatum\tBeginn\tEnde\tBrutto\tAktiv\tPause")
	var totActive, totGross time.Duration
	for i := 0; i < 7; i++ {
		day := start.AddDate(0, 0, i)
		d, err := statsForDay(db, day)
		if err != nil {
			return err
		}
		label := deDay[day.Weekday()]
		date := day.Format("02.01.")
		if !d.hasData {
			fmt.Fprintf(w, "%s\t%s\t-\t-\t-\t-\t-\n", label, date)
			continue
		}
		totActive += d.active()
		totGross += d.gross()
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			label, date, d.begin().Format("15:04"), d.end().Format("15:04"),
			fmtDur(d.gross()), fmtDur(d.active()), fmtDur(d.breaks()))
	}
	fmt.Fprintf(w, "\t\t\t\t%s\t%s\t\n", fmtDur(totGross), fmtDur(totActive))
	w.Flush()
	fmt.Println("\nSumme = Brutto/Aktiv über alle Tage mit Aktivität.")
	return nil
}

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

func main() {
	var (
		dateFlag = flag.String("date", "", "single day detail view (YYYY-MM-DD)")
		endFlag  = flag.String("end", "", "7-day overview ending on this date (YYYY-MM-DD, default today)")
		dbFlag   = flag.String("db", defaultDB(), "path to knowledgeC.db")
	)
	flag.Parse()

	const layout = "2006-01-02"

	if *dateFlag != "" {
		day, err := time.ParseInLocation(layout, *dateFlag, time.Local)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ungültiges Datum:", err)
			os.Exit(1)
		}
		d, err := statsForDay(*dbFlag, day)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		printDay(d)
		return
	}

	end := dayStart(time.Now())
	if *endFlag != "" {
		t, err := time.ParseInLocation(layout, *endFlag, time.Local)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ungültiges Datum:", err)
			os.Exit(1)
		}
		end = t
	}
	if err := printRange(*dbFlag, end); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
