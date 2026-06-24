# worktt

Arbeitszeiten aus der macOS `knowledgeC.db` ableiten. Nutzt primär den App-Nutzungs-Stream
(`/app/usage`) des lokalen Macs — mit Fallback auf den Display-Backlight-Stream
(`/display/isBacklit`) — und leitet daraus Beginn, Ende, Brutto-, Aktiv- und Pausenzeit ab.

## Build

```sh
go build -o worktt .
```

## Tests

```sh
go test ./...
```

Die Tests bauen über das `sqlite3`-CLI eine kleine Fake-`knowledgeC.db` und prüfen
den echten Query-Pfad: Geräte-Filter (Mac vs. iPhone/iPad), Merge-/Pausen-Logik und
das Clipping an der Tagesgrenze.

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
worktt -until 18:00         # Aktivität ab 18:00 ignorieren (private Abendnutzung)
worktt -db <pfad>           # andere knowledgeC.db verwenden
```

`-until HH:MM` setzt eine Tages-Obergrenze: Aktivität ab dieser Uhrzeit zählt nicht
mehr als Arbeitszeit. Nutzt du den Rechner abends noch privat, blendest du das damit
aus. Ein Block, der über die Grenze läuft (z.B. 17:00–19:00 bei `-until 18:00`), wird
an ihr abgeschnitten. Das Flag gilt für jeden Tag des Fensters und lässt sich mit
`-date`, `-end` und `-db` kombinieren.

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
- Primäre Quelle ist der `/app/usage`-Stream (Vordergrund-App-Nutzung). Trackt ein
  Mac keine App-Nutzung, fällt das Tool pro Tag auf den `/display/isBacklit`-Stream
  zurück (`ZVALUEINTEGER=1` = Display an).
- Beide Abfragen filtern auf `ZSOURCE IS NULL`, also nur Events des Geräts, auf dem
  das Tool läuft. Mit aktivem „Bildschirmzeit über Geräte teilen" (iCloud-Sync)
  liegen sonst auch Events anderer Macs/iPhones/iPads in der DB; die überlappen die
  lokalen und können einen Tag über 24h treiben.
- Zusammenhängende Segmente mit Lücken unter 60s werden zu einem Aktiv-Block gemerged
  (schnelle App-Wechsel bzw. Backlight-Flacker); Lücken ab 60s zählen als Pause.
- Isolierte aktive Segmente unter 90s fallen raus (z.B. abends kurz reingeschaut),
  damit Beginn/Ende/Brutto realistisch bleiben.
- Intervalle werden an der Tagesgrenze abgeschnitten, damit kein Segment in den
  Folgetag überläuft. Mit `-until HH:MM` wird stattdessen an dieser Uhrzeit
  abgeschnitten; Aktivität ab dann fällt komplett raus.
- **Brutto** = Ende minus Beginn. **Aktiv** = Summe der Aktiv-Blöcke. **Pause** = Brutto minus Aktiv.

## Einschränkungen

- Heutige Daten erscheinen erst, wenn macOS sie aus dem WAL ins Haupt-DB-File
  checkpointet. `immutable=1` ignoriert das WAL, daher kann „heute" kurz leer sein.
  Für vergangene Tage spielt das keine Rolle.
- Setzt voraus, dass `/usr/bin/sqlite3` vorhanden ist (auf macOS Standard).
- Misst App-Nutzung, nicht tatsächliche Arbeit: eine App im Vordergrund zählt als
  aktiv, auch wenn gerade nichts getan wird; Arbeit ohne Mac wird nicht erfasst.
