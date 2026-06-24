# AGENTS.md

Guide for agentic work on **worktt** — a small Go CLI that derives working hours
from the macOS `knowledgeC.db`.

## Overview

- Single-package Go program (`package main`), only two source files: `main.go` and
  `main_test.go`. No external Go dependencies.
- The data source is the SQLite DB `~/Library/Application Support/Knowledge/knowledgeC.db`.
  It is read **read-only through the `sqlite3` CLI** (`?immutable=1`) — deliberately no
  Go SQLite driver, to avoid CGo/deps and any lock conflict with macOS.
- User output and code comments and identifiers are all **English**.

## Build / Test / Run

```sh
go build -o worktt .     # build
go test ./...            # all tests
go vet ./...             # before committing
gofmt -l .               # check formatting (empty output = ok); gofmt -w . to fix
go run . -date 2026-06-16  # try locally
```

`go build`/`go test` run without network or setup. There is no lint/CI setup beyond
the standard Go tools — use `gofmt` and `go vet`.

## Tests

- Tests build a **throwaway `knowledgeC.db` through the `sqlite3` CLI** (`fakeDB` in
  `main_test.go`) and exercise the real query path. If `sqlite3` is not in PATH, the
  DB tests skip themselves (`t.Skip`).
- Always create times in tests via the `tm("2006-01-02 15:04:05")` helper — it parses
  in `time.Local`, matching how `statsForDay` builds the day boundaries. Never use
  `time.Now()` or similar in tests, so they stay timezone- and date-independent.
- For every new behavior add a focused test with a meaningful comment (see the style of
  the existing tests: one sentence explaining the *why*).

## Conventions & pitfalls

- **Device filter:** all DB queries must filter on `ZSOURCE IS NULL` (only events of
  this Mac). Otherwise synced events from other devices overlap and push a day past 24h.
  Do not remove.
- **Epoch:** `knowledgeC.db` stores Cocoa/Mac timestamps (2001 epoch). Always convert
  between Unix and Cocoa time via the `cocoaEpoch` constant.
- **Clipping:** intervals are clamped to `[from, to]`; `to` is midnight, or with
  `-until` the cutoff time (sentinel `untilNone` = no cutoff). This keeps any segment
  from inflating gross/active.
- **Merge/min logic:** gaps under `mergeGap` (60s) merge, isolated blocks under
  `minActive` (90s) are dropped. These constants are set on purpose — justify changes.
- **Flags:** English, lowercase names via the `flag` package (`-date`, `-end`, `-db`,
  `-until`). Input parsing errors go to `os.Stderr` and `os.Exit(1)`.
- On behavior changes **keep README.md in sync** (usage + "How it works" section).

## Commits / PRs

- Branch from `main`, short imperative commit messages (see `git log`).
- Before committing: `gofmt -l .` empty, `go vet ./...` and `go test ./...` green.
