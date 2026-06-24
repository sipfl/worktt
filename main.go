// worktt — derive working hours from macOS knowledgeC.db (app usage, with a
// display-backlight fallback).
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

// Activity is derived from foreground app usage on the local Mac (appUsageStream):
// it stays correct with the lid open and no external display, where the display
// backlight stream proved unreliable (it once logged a single 24h "on" block
// across a night). On machines that don't record app usage, statsForDay falls
// back to the backlight stream (backlitStream, ZVALUEINTEGER=1).
//
// Both queries restrict to ZSOURCE IS NULL = events recorded on this machine.
// knowledgeC also syncs events from other devices (other Macs via Screen Time
// "share across devices", and iPhone/iPad); those overlap the local ones and can
// push a day past 24h.
const (
	appUsageStream = "/app/usage"
	backlitStream  = "/display/isBacklit"
)

// sub-minute gaps between app-usage rows (rapid app switches) are merged into
// one active block; gaps at or above this threshold count as breaks.
const mergeGap = 60 * time.Second

// isolated active blocks shorter than this (e.g. a quick glance at the screen)
// are not real working time and get dropped.
const minActive = 90 * time.Second

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

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

func queryStream(db, stream string, from, to time.Time, onlyOn bool) ([]interval, error) {
	uri := "file:" + db + "?immutable=1"
	// ZSOURCE IS NULL keeps only events recorded on this machine; synced events
	// from other devices (other Macs via Screen Time, iPhone, iPad) have ZSOURCE
	// set and would otherwise overlap the local ones and inflate a day past 24h.
	cond := ""
	if onlyOn {
		// the backlight stream toggles on/off; only "on" (1) is active time.
		cond = " AND ZVALUEINTEGER=1"
	}
	q := fmt.Sprintf(`SELECT ZSTARTDATE, ZENDDATE FROM ZOBJECT `+
		`WHERE ZSTREAMNAME='%s' AND ZSOURCE IS NULL%s `+
		`AND ZSTARTDATE >= %d AND ZSTARTDATE < %d `+
		`ORDER BY ZSTARTDATE;`, stream, cond, from.Unix()-cocoaEpoch, to.Unix()-cocoaEpoch)
	out, err := exec.Command("sqlite3", "-separator", "|", uri, q).Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if strings.Contains(stderr, "unable to open database") {
			return nil, fmt.Errorf("kann %s nicht öffnen.\n"+
				"knowledgeC.db ist durch macOS geschützt: gib deinem Terminal "+
				"Full Disk Access\n(Systemeinstellungen → Datenschutz & Sicherheit "+
				"→ Festplattenvollzugriff), danach Terminal neu starten.", db)
		}
		if stderr != "" {
			return nil, fmt.Errorf("sqlite3: %s", stderr)
		}
		return nil, fmt.Errorf("sqlite3: %w", err)
	}
	var ivs []interval
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) != 2 {
			continue
		}
		s, _ := strconv.ParseFloat(f[0], 64)
		e, _ := strconv.ParseFloat(f[1], 64)
		ivs = append(ivs, interval{
			start: time.Unix(int64(s)+cocoaEpoch, 0),
			end:   time.Unix(int64(e)+cocoaEpoch, 0),
		})
	}
	return ivs, nil
}

// mergeIntervals drops zero-length rows, merges intervals separated by less
// than mergeGap (rapid app switches) and discards isolated blocks shorter than
// minActive.
func mergeIntervals(rows []interval) []interval {
	var ons []interval
	for _, r := range rows {
		if r.end.After(r.start) {
			ons = append(ons, r)
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

// untilNone is the sentinel for statsForDay's until parameter meaning "no
// end-of-day cutoff" (look at the whole day, up to midnight).
const untilNone = time.Duration(-1)

// statsForDay derives a day's activity. When until >= 0 it is a duration since
// midnight: activity at or after that time of day is ignored (e.g. private
// evening use) and a block spanning the cutoff is clipped to it.
func statsForDay(db string, day time.Time, until time.Duration) (dayStats, error) {
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	to := from.AddDate(0, 0, 1)
	if until >= 0 {
		to = from.Add(until)
	}
	// Prefer app usage; fall back to the display backlight stream on machines
	// that don't record app usage at all (no /app/usage rows for the day).
	rows, err := queryStream(db, appUsageStream, from, to, false)
	if err != nil {
		return dayStats{}, err
	}
	if len(rows) == 0 {
		rows, err = queryStream(db, backlitStream, from, to, true)
		if err != nil {
			return dayStats{}, err
		}
	}
	// Safety net: clip every interval to [from, to]. A segment can never spill
	// past the day (or, with -until set, past the cutoff) and inflate gross/active
	// (the old display stream once logged a single 24h block across a night).
	var ivs []interval
	for _, iv := range mergeIntervals(rows) {
		if iv.start.Before(from) {
			iv.start = from
		}
		if iv.end.After(to) {
			iv.end = to
		}
		if iv.end.After(iv.start) {
			ivs = append(ivs, iv)
		}
	}
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

func printRange(db string, end time.Time, until time.Duration) error {
	start := end.AddDate(0, 0, -6)
	fmt.Printf("Letzte 7 Tage (%s – %s)\n\n", start.Format("02.01.2006"), end.Format("02.01.2006"))
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "Tag\tDatum\tBeginn\tEnde\tBrutto\tAktiv\tPause")
	var totActive, totGross time.Duration
	for i := 0; i < 7; i++ {
		day := start.AddDate(0, 0, i)
		d, err := statsForDay(db, day, until)
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
		dateFlag    = flag.String("date", "", "single day detail view (YYYY-MM-DD)")
		endFlag     = flag.String("end", "", "7-day overview ending on this date (YYYY-MM-DD, default today)")
		dbFlag      = flag.String("db", defaultDB(), "path to knowledgeC.db")
		untilFlag   = flag.String("until", "", "ignore activity at/after this time of day (HH:MM), e.g. private evening use")
		versionFlag = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *versionFlag {
		fmt.Println("worktt", version)
		return
	}

	const layout = "2006-01-02"

	until, err := parseUntil(*untilFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *dateFlag != "" {
		day, err := time.ParseInLocation(layout, *dateFlag, time.Local)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ungültiges Datum:", err)
			os.Exit(1)
		}
		d, err := statsForDay(*dbFlag, day, until)
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
	if err := printRange(*dbFlag, end, until); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseUntil turns an "HH:MM" wall-clock string into a duration since midnight.
// An empty string yields untilNone (no cutoff).
func parseUntil(s string) (time.Duration, error) {
	if s == "" {
		return untilNone, nil
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("ungültige Uhrzeit %q für -until (erwartet HH:MM)", s)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}
