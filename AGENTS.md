# AGENTS.md

Leitfaden für agentisches Arbeiten an **worktt** — einem kleinen Go-CLI, das aus der
macOS-`knowledgeC.db` Arbeitszeiten ableitet.

## Überblick

- Single-Package-Go-Programm (`package main`), nur zwei Quelldateien: `main.go` und
  `main_test.go`. Keine externen Go-Dependencies.
- Datenquelle ist die SQLite-DB `~/Library/Application Support/Knowledge/knowledgeC.db`.
  Sie wird **read-only über das `sqlite3`-CLI** (`?immutable=1`) gelesen — bewusst kein
  Go-SQLite-Treiber, um ohne CGo/Deps und ohne Lock-Konflikt mit macOS auszukommen.
- Nutzer-Ausgabe ist **deutsch**, Code-Kommentare und Bezeichner sind **englisch**.

## Build / Test / Run

```sh
go build -o worktt .     # bauen
go test ./...            # alle Tests
go vet ./...             # vor dem Commit
gofmt -l .               # Formatierung prüfen (leere Ausgabe = ok); gofmt -w . zum Fixen
go run . -date 2026-06-16  # lokal ausprobieren
```

`go build`/`go test` laufen ohne Netzwerk und ohne Setup. Es gibt kein Lint-/CI-Setup
über die Standard-Go-Tools hinaus — nutze `gofmt` und `go vet`.

## Tests

- Tests bauen eine **Wegwerf-`knowledgeC.db` über das `sqlite3`-CLI** (`fakeDB` in
  `main_test.go`) und prüfen den echten Query-Pfad. Ist `sqlite3` nicht im PATH,
  überspringen sich die DB-Tests selbst (`t.Skip`).
- Zeiten in Tests immer über den Helper `tm("2006-01-02 15:04:05")` erzeugen — er parst
  in `time.Local`, passend dazu, wie `statsForDay` die Tagesgrenzen baut. Niemals
  `time.Now()` o.ä. in Tests, damit sie zeitzonen- und datumsunabhängig bleiben.
- Für jedes neue Verhalten einen fokussierten Test mit einem aussagekräftigen Kommentar
  ergänzen (siehe Stil der bestehenden Tests: ein Satz, der das *Warum* erklärt).

## Konventionen & Fallstricke

- **Geräte-Filter:** Alle DB-Abfragen müssen auf `ZSOURCE IS NULL` filtern (nur Events
  dieses Macs). Synchronisierte Events anderer Geräte überlappen sonst und treiben einen
  Tag über 24h. Nicht entfernen.
- **Epoch:** `knowledgeC.db` speichert Cocoa-/Mac-Zeitstempel (Epoche 2001). Immer über
  die Konstante `cocoaEpoch` zwischen Unix- und Cocoa-Zeit umrechnen.
- **Clipping:** Intervalle werden auf `[from, to]` geklemmt; `to` ist Mitternacht bzw.
  bei `-until` die Cutoff-Zeit (Sentinel `untilNone` = kein Cutoff). So kann kein Segment
  Brutto/Aktiv aufblähen.
- **Merge-/Min-Logik:** Lücken unter `mergeGap` (60s) verschmelzen, isolierte Blöcke unter
  `minActive` (90s) fallen raus. Diese Konstanten sind absichtlich gesetzt — Änderungen
  begründen.
- **Flags:** englische, kleingeschriebene Namen über das `flag`-Paket (`-date`, `-end`,
  `-db`, `-until`). Eingabe-Parsing-Fehler nach `os.Stderr` und `os.Exit(1)`.
- Bei Verhaltensänderungen **README.md mitpflegen** (Benutzung + Abschnitt „Wie es
  funktioniert").

## Commits / PRs

- Branch von `main`, kurze imperative Commit-Botschaften (siehe `git log`).
- Vor dem Commit: `gofmt -l .` leer, `go vet ./...` und `go test ./...` grün.
