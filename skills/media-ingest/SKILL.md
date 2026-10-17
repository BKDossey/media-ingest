---
name: media-ingest
description: Organize a folder of downloaded media into a clean library — movies as "Title (Year) [Qual]/Title (Year) [Qual].ext", TV as "Series (Year) [Qual]/Season XX/SXXEXX - Episode Title.ext", and music (MP3/FLAC) as "Artist/Album (Year)/NN - Title.ext" — dropping release-group/codec/scene junk. For music it can optionally fix tags against MusicBrainz, embed official cover art, strip junk tags, and fetch synced .lrc lyrics from LRCLIB. Use when the user wants to ingest, import, rename, organize, clean up, tag, or standardize movies, TV shows, or music files/folders, or invokes /media-ingest.
---

# Media Ingest

Turn a folder of messy downloads (movies, TV episodes, music, or a mix of all three) into a clean, consistently named library. Work in batches with few prompts: do the whole pipeline, and stop only for the decisions listed under **When to ask**.

## Setup (do this first, every run)

All the work is done by one bundled program, `media-ingest`. There's nothing to install.

1. **Find this skill's folder**, the folder containing this file. Below it's called `<skill>`.
2. **Pick the binary for this machine:** `<skill>/bin/media-ingest-<os>-<arch>`, with `.exe` on Windows.

   - `<os>`: `windows` on Windows; on macOS/Linux, `uname -s` gives `Darwin` → `darwin` or `Linux` → `linux`.
   - `<arch>`: on Windows, `PROCESSOR_ARCHITECTURE` (`$env:PROCESSOR_ARCHITECTURE` in PowerShell, `$PROCESSOR_ARCHITECTURE` in Git Bash); on macOS/Linux, `uname -m`. `AMD64` or `x86_64` → `amd64`; `ARM64`, `arm64` or `aarch64` → `arm64`.

   For example: `media-ingest-windows-amd64.exe`, `media-ingest-darwin-arm64` (Apple Silicon Mac), or `media-ingest-linux-amd64`.

   Below, `<mi>` means the full, quoted path to that binary.
3. **macOS/Linux only:** if it isn't executable, run `chmod +x "<mi>"`.
4. **Check it runs:** `"<mi>" version`. If it can't run (no binary for this platform, or the OS blocks it) and `go` is installed, use `go -C "<repo>" run ./cmd/media-ingest <args>` instead, where `<repo>` is the folder two levels above `<skill>` (it contains `go.mod`). If neither works, tell the user what's missing: Go from https://go.dev/dl, or that their platform isn't supported.

Then:
- **Always pass absolute, quoted paths.** Release names contain `[`, `]`, `(`, `)`, `'` and `&`.
- **Scratch files** (draft/plan JSON, cover downloads, manifests) go in a temp or scratchpad directory, never in the source folder.
- **Every command has `-h`.** `"<mi>" help` lists them all.

## Pipeline

### 1. Find the folder and read the config
Use the folder the user gave. If they gave none, use `source` from the config. If neither exists, ask. Run `"<mi>" config show`. The config is optional and lives at `~/.config/media-ingest/config.json`; see [references/config.md](references/config.md).

### 2. Survey
```bash
"<mi>" scan "<folder>"
```
This sorts everything into video, audio, companions (subtitles, .nfo, lyrics), and other. `audio_unsupported` lists audio that isn't MP3 or FLAC. Those files are left alone; mention them in the report. If there's nothing to ingest, say so and stop.

### 3. Settle the options in one go
For each media type found, look at its config options. Ask about every option set to `"ask"` in **one** message, using a multiple-choice question tool if available. Skip questions for types not present.
- **TV found** and `video.episode_titles` is `"ask"`: look up episode titles on TVmaze, or name episodes `SXXEXX` only?
- **Music found** and `music.metadata` is `"ask"`: fix tags against MusicBrainz, embed official covers, and clean junk tags? Or just rename using the tags the files already have?
- **Music found** and `music.lyrics` is `"ask"`: fetch lyrics from LRCLIB? Mention whether plain lyrics are also saved (`music.plain_lyrics`).

Mention where each type will land (`destinations`, or in place in the source folder when unset), so a wrong destination gets caught now.

### 4. Movies and TV
Follow [references/video.md](references/video.md).

### 5. Music
Follow [references/music.md](references/music.md).

### 6. Follow-ups
If the config's `after_ingest` list has entries, carry them out now, in order. They are the user's own follow-up steps, such as running another skill on the new music. Skip any that don't apply to the media types processed this run.

### 7. Report
Give one short section per media type:
- **Video:** old name → new path, episode titles filled in, and leftover folders that still hold files (with what's in them).
- **Music:** a table per album with file → new path, what changed (year, capitalization, cover, track numbers, junk removed), and the lyrics result (synced / plain / none). Flag MP3s under 192 kbps as worth replacing.
- Anything guessed, skipped, unsupported, or unresolved.

## Shared rules

- **Draft, review, then act.** The draft commands change nothing. Review every draft before applying it. Fix wrong fields by editing the draft or plan JSON, never by renaming files by hand.
- **Never overwrite, never delete.** The program refuses to overwrite an existing file. Source folders are removed only once completely empty. Replaced music goes to `<music library>/.replaced/`, and duplicates go to `<source>/.duplicates/`. Nothing is deleted outright, except a `.txt` plain-lyrics file once a synced `.lrc` replaces it.
- **Moves across drives or NAS mounts** are copied, checksum-verified, and only then removed from the source.
- **Hidden files and folders** (names starting with `.`) are always ignored.

## When to ask (otherwise just proceed)

- An option is set to `"ask"` (step 3, all at once).
- A movie's year or a video's quality can't be determined, and lookup or `ffprobe` doesn't settle it.
- Several TV shows share a name and the year doesn't settle which one it is.
- MusicBrainz offers several plausible releases that differ in track count or year, and the song length doesn't settle it.
- A music file has too few tags to identify it, and its filename doesn't help.
- A destination already has the file (a conflict), or a music upgrade is pending and the user hasn't said they're replacing songs.
- A downloaded cover looks wrong and there is no alternative.
