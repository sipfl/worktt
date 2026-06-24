# worktt

Derive working hours from the macOS `knowledgeC.db`. Uses the local Mac's app-usage
stream (`/app/usage`) as the primary source, with a fallback to the display-backlight
stream (`/display/isBacklit`), and reports start, end, gross, active and break time.

## Installation

```sh
brew install sipfl/tap/worktt
```

This taps `sipfl/tap` automatically. Update later with `brew upgrade worktt`.

Then grant Full Disk Access (see below) — without it the tool cannot read the
protected database.

## Prerequisite: Full Disk Access

`knowledgeC.db` is protected by macOS (TCC). The terminal program running the tool
needs **Full Disk Access**, otherwise opening it fails with `unable to open database file`:

1. System Settings → Privacy & Security → Full Disk Access
2. Add and enable your terminal (Terminal / iTerm / Ghostty / …)
3. Restart the terminal

This is tied to the terminal, not to the tool itself.

## Usage

```sh
worktt                      # last 7 days (default, ending today)
worktt -end 2026-06-17      # 7-day window ending on this date
worktt -date 2026-06-16     # single-day detail with interval table
worktt -until 18:00         # ignore activity from 18:00 on (private evening use)
worktt -db <path>           # use a different knowledgeC.db
```

`-until HH:MM` sets a daily cutoff: activity from this time on no longer counts as
working time. If you keep using the machine privately in the evening, this excludes
it. A block that spans the cutoff (e.g. 17:00–19:00 with `-until 18:00`) is clipped
to it. The flag applies to every day in the window and combines with `-date`, `-end`
and `-db`.

### Overview (default)

```
Last 7 days (18.06.2026 – 24.06.2026)

Day  Date    Start  End    Gross    Active   Break
...
                           15h 19m  12h 22m
```

### Single-day detail (`-date`)

```
Mon, 15.06.2026
──────────────────────
From   To     Length  Status
07:15  07:41  26m     active
07:41  07:57  17m     break
...

Start:  07:15:36
End:    14:55:28
Gross:  7h 40m
Active: 6h 33m
Break:  1h 07m
```

## Build from source

```sh
go build -o worktt .
```

Install globally instead (lands in `$GOPATH/bin`, usually `~/go/bin`):

```sh
go install .
```

### Tests

```sh
go test ./...
```

The tests build a small fake `knowledgeC.db` through the `sqlite3` CLI and exercise
the real query path: device filter (Mac vs. iPhone/iPad), merge/break logic and the
clipping at the day boundary.

## How it works

- Reads `~/Library/Application Support/Knowledge/knowledgeC.db` read-only through the
  `sqlite3` CLI with `immutable=1`. No lock conflict with the running macOS process,
  no external Go dependencies.
- The primary source is the `/app/usage` stream (foreground app usage). If a Mac does
  not track app usage, the tool falls back per day to the `/display/isBacklit` stream
  (`ZVALUEINTEGER=1` = display on).
- Both queries filter on `ZSOURCE IS NULL`, i.e. only events of the device the tool
  runs on. With "share across devices" (iCloud sync) enabled, the DB also holds events
  from other Macs/iPhones/iPads; those overlap the local ones and can push a day past 24h.
- Contiguous segments separated by gaps under 60s are merged into one active block
  (rapid app switches or backlight flicker); gaps of 60s or more count as a break.
- Isolated active segments under 90s are dropped (e.g. a quick glance in the evening),
  so start/end/gross stay realistic.
- Intervals are clipped at the day boundary so no segment spills into the next day.
  With `-until HH:MM` the clipping happens at that time instead; activity from then on
  is dropped entirely.
- **Gross** = end minus start. **Active** = sum of the active blocks. **Break** = gross minus active.

## Limitations

- Today's data only shows up once macOS checkpoints it from the WAL into the main DB
  file. `immutable=1` ignores the WAL, so "today" may briefly be empty. This does not
  affect past days.
- Requires `/usr/bin/sqlite3` (standard on macOS).
- Measures app usage, not actual work: a foreground app counts as active even when
  nothing is being done; work away from the Mac is not captured.
