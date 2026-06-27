# Tefnut

A self-hosted family comic server (Plex-for-comics). Point it at a directory of
comic archives (`.zip/.cbz`, `.rar/.cbr`, `.7z/.cb7`) and read them in your
browser. Single Go binary + a SQLite file; no external database.

## Quick start

1. Edit `cmd/tefnut/config.yaml` (or copy it next to the binary):
   - `library.rootPath` — your comic library directory
   - `dataDir` — where the DB, thumbnails, and extract cache live
   - `server.addr` — listen address (default `:8086`)
   - `scan.interval` — rescan period (default `2m`)
   - `thumbnail.width` — cover width in px (default `400`)
2. Run:
   ```bash
   go run ./cmd/tefnut -config ./cmd/tefnut/config.yaml
   ```
3. Open http://localhost:8086

Drop new comic archives into the library directory; they appear after the next
scan (and immediately on restart).

## Features
- Folder-based browsing of the library tree
- Auto-generated cover thumbnails (first page of each archive)
- In-browser reader with keyboard paging and remembered progress
- Per-comic author, 0–5★ rating, and free-text tags
- Search by name; filter by tag and minimum rating
- Tag management page (rename / delete / counts)

## Build
```bash
go build -o tefnut ./cmd/tefnut
```
