# media-ingest

A [Claude Code](https://claude.com/claude-code) skill that turns a folder of messy downloads (movies, TV episodes, music, or a mix) into a clean, consistently named media library. It runs on Windows, macOS, and Linux with **nothing to install**: the skill ships a small self-contained program for each platform.

```
Peter.Pan.2003.1080p.mkv
  → Peter Pan (2003) [1080p]/Peter Pan (2003) [1080p].mkv

Silo.S02.1080p/silo.s02e01.1080p.mkv
  → Silo (2023) [1080p]/Season 02/S02E01 - The Engineer.mkv

02 nutshell.mp3   (artist "Alice In Chains", album "Jar Of Flies (Remastered)", no cover)
  → Alice in Chains/Jar of Flies (1993)/02 - Nutshell.mp3   (+ cover art, + 02 - Nutshell.lrc)
```

Install it, type `/media-ingest`, and Claude takes it from there: it sorts the folder by type, asks what it needs to, drafts the new names for you to review, and then moves the files. It never overwrites and never deletes.

---

## Contents

- [What it does](#what-it-does)
- [Naming standard](#naming-standard)
- [Install](#install)
- [Usage](#usage)
- [Configuration](#configuration)
- [Chaining your own follow-up steps](#chaining-your-own-follow-up-steps)
- [Safety](#safety)
- [How it works](#how-it-works)
- [Command-line reference](#command-line-reference)
- [Building from source](#building-from-source)
- [Online services used](#online-services-used)
- [Troubleshooting](#troubleshooting)
- [License](#license)

## What it does

**Movies and TV**
- Strips technical clutter from names: codecs, sources, audio specs, bit depth, and other tags.
- Detects title, year, quality (`2160p` / `1080p` / `720p` / `480p`), and season/episode, including multi-episode files (`S01E01-E02`) and `1x05`-style numbering.
- Keeps intentional dots (`A.I.`, `S.W.A.T.`) and country tags (`The Office (US)`).
- Optionally looks up canonical series names, first-air years, and **episode titles** on TVmaze.
- Carries subtitles and `.nfo` files along, renamed to match (`Movie (2003) [1080p].en.srt`).
- Skips sample and trailer clips, and reports leftover junk instead of silently deleting it.
- Falls back to `ffprobe`, if installed, to read the resolution when the name doesn't say.

**Music (MP3 and FLAC)**
- Organizes into `Artist/Album (Year)/NN - Title.ext`.
- **Optional metadata fixing:** corrects tags against MusicBrainz (canonical spelling and capitalization, original release year, track/disc numbers, album artist), embeds the official front cover from the Cover Art Archive, and strips junk tags and stray release IDs that make servers like Navidrome, Jellyfin, or Plex split one album into several. MP3 tags are saved as ID3v2.3, the most widely readable version.
- **Optional lyrics:** fetches synced `.lrc` lyrics from LRCLIB, and optionally plain `.txt` lyrics as a fallback.
- Detects **format upgrades** (a new FLAC for an MP3 you already have) and swaps them in only when asked, setting the old file aside rather than deleting it.

Both music options can be turned on or off in the config, or Claude asks you each run. Other audio formats (M4A, OGG, WMA…) are reported and left alone.

## Naming standard

| Type | Layout |
|---|---|
| Movie | `Title (Year) [Qual]/Title (Year) [Qual].ext` |
| TV | `Series Title (Year) [Qual]/Season XX/SXXEXX - Episode Title.ext` |
| Music | `Album Artist/Album (Year)/NN - Title.ext` (multi-disc: `D-NN - Title.ext`) |

- `(Year)` is the movie's release year, the series' first-air year, or the album's original release year.
- `[Qual]` is the resolution only.
- Lyrics sit next to the song with the same name: `NN - Title.lrc`.

## Install

You need [Claude Code](https://claude.com/claude-code) (CLI, desktop app, or IDE extension) and nothing else. [`ffprobe`](https://ffmpeg.org/) (part of FFmpeg) is optional; it reads video resolution when a file name doesn't include it.

### Option A: as a plugin (recommended)
In Claude Code:
```
/plugin marketplace add BKDossey/media-ingest
/plugin install media-ingest@media-ingest
```
Run `/plugin marketplace update media-ingest` to pull updates. Installing the plugin the same way on each of your machines gets you the same skill everywhere.

### Option B: copy the skill folder
```bash
git clone https://github.com/BKDossey/media-ingest.git
```
Then copy `media-ingest/skills/media-ingest/` into your personal skills folder:

| OS | Path |
|---|---|
| macOS / Linux | `~/.claude/skills/media-ingest/` |
| Windows | `%USERPROFILE%\.claude\skills\media-ingest\` |

To make it available in a single project only, put it in that project's `.claude/skills/` instead.

### Supported platforms
The `bin/` folder includes builds for Windows, macOS, and Linux, each on x86-64 (`amd64`) and ARM (`arm64`). Claude picks the right one. On anything else, the skill falls back to `go run` if [Go](https://go.dev/dl) is installed.

## Usage

Just ask, or invoke it by name:
```
/media-ingest ~/Downloads/complete
ingest everything in D:\Incoming
rename the movies in /mnt/media/new
tag and organize the albums I just downloaded, with lyrics
```

A run goes like this:
1. **Survey:** the folder is sorted into video, audio, companions, and other.
2. **Options:** anything set to `"ask"` in your config is asked once, all together (episode titles? fix music metadata? fetch lyrics?).
3. **Draft:** the new name for every file is drafted and shown to you, with notes on anything uncertain.
4. **Apply:** files are moved into the library layout, and leftovers are reported.
5. **Report:** a summary per media type of what changed, what was guessed, and anything left for you.

## Configuration

Optional. It lives **outside** the skill folder, so your paths and preferences never end up in the repo:

```
~/.config/media-ingest/config.json          (Windows: C:\Users\<you>\.config\media-ingest\config.json)
```

Ask Claude to "set up my media-ingest config", or create a starter file yourself with `media-ingest config init`. Example:

```json
{
  "source": "~/Downloads/complete",
  "destinations": {
    "movies": "/Volumes/NAS/Movies",
    "tv": "/Volumes/NAS/TV Shows",
    "music": "/Volumes/NAS/Music"
  },
  "video": { "episode_titles": true },
  "music": { "metadata": true, "lyrics": true, "plain_lyrics": false },
  "contact": null,
  "after_ingest": []
}
```

| Key | Default | Meaning |
|---|---|---|
| `source` | `null` | Folder to process when you don't name one |
| `destinations.*` | `null` | Library root for movies, TV, or music. `null` organizes in place inside the source folder |
| `video.episode_titles` | `"ask"` | `true` / `false` / `"ask"`: look up TV episode titles on TVmaze |
| `music.metadata` | `"ask"` | `true` / `false` / `"ask"`: MusicBrainz tag fix, cover art, junk cleanup |
| `music.lyrics` | `"ask"` | `true` / `false` / `"ask"`: fetch lyrics from LRCLIB |
| `music.plain_lyrics` | `false` | Save unsynced lyrics as `.txt` when no synced version exists |
| `contact` | `null` | Email or URL added to the MusicBrainz User-Agent (optional, but MusicBrainz appreciates it) |
| `after_ingest` | `[]` | Your own follow-up steps, in plain language (see below) |

The full reference is in [`skills/media-ingest/references/config.md`](skills/media-ingest/references/config.md).

## Chaining your own follow-up steps

Plenty of people do something after organizing: upload to a NAS over SSH, trigger a Plex or Jellyfin scan, or add new songs to a playlist. Those steps are personal, so they don't belong in a public skill. Instead:

1. Write a small **private** skill on your own machine (e.g. `~/.claude/skills/my-music-upload/`) that does your step.
2. Point `after_ingest` at it:
   ```json
   "after_ingest": ["For music: run the my-music-upload skill on the newly organized albums"]
   ```

`media-ingest` runs those steps at the end of each ingest, so your setup stays private while this skill stays generic.

## Safety

- **Draft first.** Nothing moves until the plan has been reviewed. Every command that changes files has `--dry-run`.
- **Never overwrites.** An existing destination file is reported as a conflict and the source is left alone.
- **Never deletes.** Source folders are removed only once completely empty. Folders still holding junk (stray `.txt` files, `Sample/`) are listed for you to decide on. Replaced music goes to `<library>/.replaced/` and duplicates to `<source>/.duplicates/`. The one exception is a plain `.txt` lyrics file, which is replaced when a synced `.lrc` for the same song is found.
- **Verified cross-drive moves.** Moving to another drive or a mounted NAS copies the file, compares MD5 checksums, and only then removes the source.
- **Safe tag writes.** FLAC files are rewritten to a temporary file and swapped in only once complete.
- **Hidden files are ignored.** Anything starting with `.` is skipped.

## How it works

`SKILL.md` is the entry point Claude reads. The detailed procedures sit in `references/` and are loaded only when that media type is present. The deterministic work (parsing names, lookups, tag writing, moving files) is done by the `media-ingest` program, which is written in Go:

```
media-ingest/
├── .claude-plugin/            plugin + marketplace manifests
├── skills/media-ingest/       the skill (this is what gets installed)
│   ├── SKILL.md               entry point: setup, pipeline, options, shared rules
│   ├── references/
│   │   ├── video.md           movie/TV rules and procedure
│   │   ├── music.md           music rules, MusicBrainz workflow, metadata checklist
│   │   └── config.md          every config option
│   └── bin/                   prebuilt media-ingest for 6 OS/CPU combinations
├── cmd/media-ingest/          main: subcommand dispatch, scan, config, tv
├── internal/
│   ├── core/                  config, file walking, safe moves, HTTP, argument parsing
│   ├── video/                 release-name parser, TVmaze, draft-video, organize-video
│   └── music/                 MP3/FLAC tags, MusicBrainz, LRCLIB, draft/apply-music
└── tools/build/               cross-compiles every binary into skills/media-ingest/bin/
```

## Command-line reference

The program also works on its own. Run `media-ingest help`, or `media-ingest <command> -h` for details.

| Command | What it does |
|---|---|
| `scan <folder>` | Sort a folder into video / audio / companions / other (JSON) |
| `config show\|path\|init` | Show, locate, or create the config file |
| `draft-video <folder> <out.json> [--episode-titles]` | Draft library names for every video; nothing is renamed |
| `organize-video <draft.json> [--dry-run]` | Apply a reviewed video draft |
| `tv search <name>` / `tv episodes --show <id>` | TVmaze lookups |
| `inspect <folder>` | Dump every tag, picture, and lyrics sidecar (JSON) |
| `draft-music <folder> <out.json>` | Draft canonical MusicBrainz tags for every song |
| `mb album\|track\|detail\|cover …` | MusicBrainz lookups and cover download |
| `apply-music <plan.json>` | Write tags and cover, strip junk, move into the library |
| `apply-music --rename-only <folder>` | Move into the library using existing tags only |
| `lyrics <folder>` / `lyrics --files <list>` | Fetch lyrics from LRCLIB |
| `release-tags <folder> [--dry-run]` | Strip release-specific IDs from an existing library |

## Building from source

Needs [Go](https://go.dev/dl) 1.22 or newer.

```bash
go test ./...                     # run the tests
go run ./cmd/media-ingest help    # run without building
go run ./tools/build              # cross-compile all six binaries into skills/media-ingest/bin/
```

`tools/build` sets the executable bit on the macOS/Linux binaries in git, so checkouts on those systems can run them directly. Rebuild and commit the binaries whenever you tag a release. There's no CI involved, so it works on any git host.

Dependencies: [`bogem/id3v2`](https://github.com/bogem/id3v2) (MP3 tags) and [`go-flac`](https://github.com/go-flac) (FLAC tags and pictures). Everything else is Go's standard library. media-ingest writes its own ID3 text, TXXX, APIC, and USLT frames, because `bogem/id3v2` adds a stray byte after UTF-16 text that corrupts frames with more than one field.

## Online services used

All are free and need no API key:

| Service | Used for | Notes |
|---|---|---|
| [TVmaze](https://www.tvmaze.com/api) | Series names, first-air years, episode titles | Only when episode titles are on |
| [MusicBrainz](https://musicbrainz.org/doc/MusicBrainz_API) | Canonical music metadata | Only when music metadata fixing is on; rate-limited to ~1 request/second |
| [Cover Art Archive](https://coverartarchive.org/) | Official album covers | Only when music metadata fixing is on |
| [LRCLIB](https://lrclib.net/) | Synced and plain lyrics | Only when lyrics are on |

Requests send only search terms (titles, artists, durations) and a generic `media-ingest/<version>` User-Agent. Nothing about your files or system is uploaded.

## Troubleshooting

| Problem | Fix |
|---|---|
| macOS says the binary "cannot be opened" | Only happens if it was downloaded through a browser. Run `xattr -d com.apple.quarantine skills/media-ingest/bin/media-ingest-darwin-*`, or install via the plugin or `git clone` |
| `permission denied` running the binary on macOS/Linux | `chmod +x skills/media-ingest/bin/media-ingest-*` (Claude does this automatically) |
| "quality unknown" for many videos | Install FFmpeg so `ffprobe` can read the resolution, or tell Claude the quality |
| Wrong TV show picked (remakes, `(US)` vs `(UK)`) | Fix `title` / `year` in the draft, or check `media-ingest tv search "name"` |
| One album shows up twice in your music server | Inconsistent tags or leftover release IDs. Run `media-ingest release-tags <library> --dry-run`, then rescan |
| A song wasn't moved: `CONFLICT` | The exact file already exists in the library. Decide which copy to keep |
| A song wasn't moved: `UPGRADE` | The same song exists in another format. Re-run with `--replace` to swap it in |
| An `.m4a`/`.ogg` file was skipped | Only MP3 and FLAC are supported |

## License

[MIT](LICENSE)
