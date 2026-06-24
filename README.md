# worktt

Arbeitszeiten aus der macOS `knowledgeC.db` ableiten. Liest den Display-Backlight-Stream
(`/display/isBacklit`) und leitet daraus Beginn, Ende, Brutto-, Aktiv- und Pausenzeit ab.

## Build

```sh
go build -o worktt .
```

Optional global installieren (landet in `$GOPATH/bin`, meist `~/dev/go/bin`):

```sh
go install .
```

## Voraussetzung: Full Disk Access

`knowledgeC.db` ist durch macOS (TCC) geschützt. Das ausführende Terminal-Programm
braucht **Full Disk Access**, sonst scheitert das Öffnen mit `unable to open database file`:

1. Systemeinstellungen → Datenschutz & Sicherheit → Festplattenvollzugriff
2. Terminal (bzw. iTerm/Ghostty/…) hinzufügen und aktivieren
3. Terminal neu starten

Das hängt am Terminal, nicht am Tool selbst.

## Benutzung

```sh
worktt                      # letzte 7 Tage (default, endet heute)
worktt -end 2026-06-17      # 7-Tage-Fenster endend an diesem Datum
worktt -date 2026-06-16     # Tagesdetail mit Intervall-Tabelle
worktt -db <pfad>           # andere knowledgeC.db verwenden
```

### Übersicht (default)

```
Letzte 7 Tage (18.06.2026 – 24.06.2026)

Tag  Datum   Beginn  Ende   Brutto   Aktiv    Pause
...
                            15h 19m  12h 22m
```

### Tagesdetail (`-date`)

```
Mo, 15.06.2026
──────────────────────
Von    Bis    Dauer   Status
07:15  07:41  26m     aktiv
07:41  07:57  17m     Pause
...

Beginn:  07:15:36
Ende:    14:55:28
Brutto:  7h 40m
Aktiv:   6h 33m
Pause:   1h 07m
```

## Wie es funktioniert

- Liest `~/Library/Application Support/Knowledge/knowledgeC.db` read-only über das
  `sqlite3`-CLI mit `immutable=1`. Kein Lock-Konflikt mit dem laufenden macOS-Prozess,
  keine externen Go-Dependencies.
- Quelle ist der `/display/isBacklit`-Stream (Display an/aus).
- Zusammenhängende „an"-Phasen mit Lücken unter 60s werden gemerged (Flacker).
- Isolierte aktive Segmente unter 90s fallen raus (z.B. abends kurz reingeschaut),
  damit Beginn/Ende/Brutto realistisch bleiben.
- **Brutto** = Ende minus Beginn. **Aktiv** = Summe der „an"-Phasen. **Pause** = Brutto minus Aktiv.

## Einschränkungen

- Heutige Daten erscheinen erst, wenn macOS sie aus dem WAL ins Haupt-DB-File
  checkpointet. `immutable=1` ignoriert das WAL, daher kann „heute" kurz leer sein.
  Für vergangene Tage spielt das keine Rolle.
- Setzt voraus, dass `/usr/bin/sqlite3` vorhanden ist (auf macOS Standard).
- Misst Display-Aktivität, nicht tatsächliche Arbeit: ein offener Bildschirm ohne
  Tätigkeit zählt als aktiv, externe Arbeit ohne Mac wird nicht erfasst.
