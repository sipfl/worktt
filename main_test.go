package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// row is one ZOBJECT record for the fake knowledgeC.db.
type row struct {
	stream     string
	src        int // ZSOURCE.Z_PK: 1 = local Mac, 2 = synced iPhone
	start, end time.Time
}

// tm parses a local wall-clock time the same way statsForDay builds its day
// bounds (time.Local), so test data and queries line up regardless of host TZ.
func tm(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

// fakeDB builds a throwaway knowledgeC.db with just the columns the tool reads.
// ZSOURCE 1 is the local Mac (ZDEVICEID NULL); ZSOURCE 2 is a synced device.
func fakeDB(t *testing.T, rows []row) string {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	db := filepath.Join(t.TempDir(), "knowledgeC.db")
	var b strings.Builder
	b.WriteString(`CREATE TABLE ZSOURCE (Z_PK INTEGER PRIMARY KEY, ZDEVICEID VARCHAR);`)
	b.WriteString(`INSERT INTO ZSOURCE (Z_PK, ZDEVICEID) VALUES (1, NULL);`)          // local Mac
	b.WriteString(`INSERT INTO ZSOURCE (Z_PK, ZDEVICEID) VALUES (2, 'IPHONE-UUID');`) // synced
	b.WriteString(`CREATE TABLE ZOBJECT (Z_PK INTEGER PRIMARY KEY, ZSTREAMNAME VARCHAR, ` +
		`ZSTARTDATE REAL, ZENDDATE REAL, ZVALUEINTEGER INTEGER, ZVALUESTRING VARCHAR, ZSOURCE INTEGER);`)
	for i, r := range rows {
		fmt.Fprintf(&b, `INSERT INTO ZOBJECT (Z_PK, ZSTREAMNAME, ZSTARTDATE, ZENDDATE, ZSOURCE) `+
			`VALUES (%d, '%s', %d, %d, %d);`,
			i+1, r.stream, r.start.Unix()-cocoaEpoch, r.end.Unix()-cocoaEpoch, r.src)
	}
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake db: %v\n%s", err, out)
	}
	return db
}

func TestStatsForDay(t *testing.T) {
	rows := []row{
		// local Mac, morning — two rows 30s apart merge into one active block
		{"/app/usage", 1, tm("2026-06-24 06:50:00"), tm("2026-06-24 06:52:00")},
		{"/app/usage", 1, tm("2026-06-24 06:52:30"), tm("2026-06-24 06:55:00")},
		// later block after a >60s gap -> counts as a break in between
		{"/app/usage", 1, tm("2026-06-24 08:00:00"), tm("2026-06-24 09:00:00")},
		// isolated 60s blip -> dropped (< minActive)
		{"/app/usage", 1, tm("2026-06-24 22:00:00"), tm("2026-06-24 22:01:00")},
		// synced iPhone usage -> excluded by the device filter
		{"/app/usage", 2, tm("2026-06-24 07:00:00"), tm("2026-06-24 07:45:00")},
		// other stream -> excluded
		{"/display/isBacklit", 1, tm("2026-06-24 05:00:00"), tm("2026-06-24 06:00:00")},
	}
	d, err := statsForDay(fakeDB(t, rows), tm("2026-06-24 00:00:00"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(d.ivs), 2; got != want {
		t.Fatalf("intervals = %d, want %d (%v)", got, want, d.ivs)
	}
	if got, want := d.begin(), tm("2026-06-24 06:50:00"); !got.Equal(want) {
		t.Errorf("begin = %s, want %s", got, want)
	}
	if got, want := d.end(), tm("2026-06-24 09:00:00"); !got.Equal(want) {
		t.Errorf("end = %s, want %s", got, want)
	}
	if got, want := d.active(), 65*time.Minute; got != want {
		t.Errorf("active = %s, want %s", got, want)
	}
	if got, want := d.gross(), 2*time.Hour+10*time.Minute; got != want {
		t.Errorf("gross = %s, want %s", got, want)
	}
	if got, want := d.breaks(), 65*time.Minute; got != want {
		t.Errorf("breaks = %s, want %s", got, want)
	}
}

// A segment that runs past midnight must be clipped to the day, so it can never
// inflate the day's totals (the bug the old display stream produced).
func TestStatsForDayClipsMidnight(t *testing.T) {
	rows := []row{
		{"/app/usage", 1, tm("2026-06-24 23:30:00"), tm("2026-06-25 00:30:00")},
	}
	d, err := statsForDay(fakeDB(t, rows), tm("2026-06-24 00:00:00"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := d.end(), tm("2026-06-25 00:00:00"); !got.Equal(want) {
		t.Errorf("end = %s, want %s (clipped to midnight)", got, want)
	}
	if got, want := d.active(), 30*time.Minute; got != want {
		t.Errorf("active = %s, want %s", got, want)
	}
}

// queryIntervals must return only local-Mac rows of the chosen stream.
func TestQueryIntervalsDeviceFilter(t *testing.T) {
	rows := []row{
		{"/app/usage", 1, tm("2026-06-24 09:00:00"), tm("2026-06-24 10:00:00")},
		{"/app/usage", 2, tm("2026-06-24 09:00:00"), tm("2026-06-24 10:00:00")},
	}
	from := tm("2026-06-24 00:00:00")
	ivs, err := queryIntervals(fakeDB(t, rows), from, from.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(ivs), 1; got != want {
		t.Fatalf("intervals = %d, want %d (synced device should be filtered)", got, want)
	}
	if got, want := ivs[0].start, tm("2026-06-24 09:00:00"); !got.Equal(want) {
		t.Errorf("start = %s, want %s", got, want)
	}
}

func TestMergeIntervals(t *testing.T) {
	in := []interval{
		{tm("2026-06-24 11:00:00"), tm("2026-06-24 11:00:30")}, // isolated 30s -> dropped
		{tm("2026-06-24 10:00:00"), tm("2026-06-24 10:02:00")}, // out of order on purpose
		{tm("2026-06-24 10:02:30"), tm("2026-06-24 10:05:00")}, // 30s gap -> merges with prev
		{tm("2026-06-24 12:00:00"), tm("2026-06-24 12:00:00")}, // zero-length -> dropped
	}
	out := mergeIntervals(in)
	if got, want := len(out), 1; got != want {
		t.Fatalf("intervals = %d, want %d (%v)", got, want, out)
	}
	if !out[0].start.Equal(tm("2026-06-24 10:00:00")) || !out[0].end.Equal(tm("2026-06-24 10:05:00")) {
		t.Errorf("merged block = %s–%s, want 10:00:00–10:05:00", out[0].start, out[0].end)
	}
}
