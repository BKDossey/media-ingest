# Movies and TV

## Target formats (copy exactly)

**Movies**: a folder and a file, named identically:
```
Title (Year) [Qual]/
  Title (Year) [Qual].ext
```
Example: `Peter Pan (2003) [1080p]/Peter Pan (2003) [1080p].mkv`

**TV series**:
```
Series Title (Year) [Qual]/
  Season XX/
    SXXEXX - Episode Title.ext
```
Example: `Silo (2023) [1080p]/Season 02/S02E01 - The Engineer.mkv`

Nothing else goes in the names: no `BluRay`, `x265`, `HEVC`, `10bit`, `AAC`, `5.1`, `WEBRip`, `YTS`, `RARBG`, `-Tigole`, `[YTS.AG]`, and so on. All of it is dropped.

## Field rules

- **Title:** convert dots and underscores to spaces. Keep intentional dots in abbreviations (`A.I. Artificial Intelligence`, `S.W.A.T.`). Fix obvious punctuation toward the real title when you're confident (`Hes Just Not That Into You` → `He's Just Not That Into You`), but stay conservative and never invent. Keep country tags that are part of a series name, such as `The Office (US)`.
- **(Year):** always in parentheses. For movies, the release year. For TV, the year the series first aired, even for later seasons.
- **[Qual]:** resolution only, one of `[2160p]` (also 4K/UHD), `[1080p]`, `[720p]`, `[480p]` (also 576p/SD). If nothing in the name says, the drafter tries `ffprobe` when it's installed. Otherwise ask; don't guess.
- **Season XX / SXXEXX:** two digits (`Season 03`, `S03E01`), or three only if a number exceeds 99. Multi-episode files are written `S01E01-E02`.
- **Episode Title:** proper-cased, spaces not dots. If titles are off (by config or the user's answer), the file is just `SXXEXX.ext`.
- **.ext:** keep the original extension. Companion files (subtitles, `.nfo`) are renamed to match: `Title (Year) [Qual].en.srt`, `.forced.srt`, `.nfo`, and so on.

## Procedure

### 1. Draft
```bash
"<mi>" draft-video "<folder>" "<scratch>/video.json" [--episode-titles]
```
Pass `--episode-titles` when episode titles are wanted. It looks each series up on TVmaze (free, no key) and fills in the canonical series name, first-air year, and every episode title. The drafter:
- parses title, year, quality, and season/episode from each file name and its release folder;
- picks up companion files: same-stem subtitles and `.nfo` next to the video, plus loose subtitles (e.g. `Subs/English.srt`) in a single-video release folder;
- skips `sample`/`trailer` clips;
- prints the proposed path for every video, plus **notes** for anything uncertain.

Destinations come from `--movies-dest` / `--tv-dest`, then the config, then the source folder itself (in place).

### 2. Review and fix the draft
Read every line of the printout. Edit `video.json` to fix anything wrong:
- **Notes to act on:** `year unknown` (for a movie, look it up with web search if available, or ask); `quality unknown` (ask); `several shows named …` (confirm the year; a remake or a different country's version is a different show); `no TVmaze match` / `episode not found` (search with `"<mi>" tv search`, or ask whether to leave the title off).
- **Titles:** fix punctuation and capitalization toward the real title.
- **Not actually video for the library** (extras, featurettes, a stray clip): set `"skip": true`.
- **Movie vs TV:** an item is TV only if its name has an episode marker (`S01E02`, `1x02`). A loose file with no marker inside a season folder needs its `type`, `season` and `episode` set by hand.

Useful lookups:
```bash
"<mi>" tv search "the office"
"<mi>" tv episodes --show <id> --season 2
```
If the user asked for titles and TVmaze lacks them, use web search when available. Never invent episode titles. If none can be found, leave `episode_title` empty and report it.

### 3. Show the plan, then apply it
Show the user the old → new table, then run:
```bash
"<mi>" organize-video "<scratch>/video.json"
```
Use `--dry-run` first if anything in the plan is unusual. The script builds every name from the item fields, moves the video and its companions, and never overwrites. It removes source folders only once they're completely empty, and lists folders that still hold something, with their contents (typically release-site `.txt` files, `Sample/` folders, screenshots). Report those leftovers. **Don't delete them yourself**, and don't hand the user a delete command unless they ask for one.

## Worked examples

| Source | Result |
|---|---|
| `Peter Pan (2003) (1080p BluRay x265 HEVC 10bit AAC 5.1 Tigole)` | `Peter Pan (2003) [1080p]/Peter Pan (2003) [1080p].mkv` |
| `Hachi.A.Dog's.Tale.2009.BluRay.1080p.x264.AAC.5.1.-.Hon3y` | `Hachi A Dog's Tale (2009) [1080p]/…` |
| `The.Impossible.2012.1080p.BluRay.H264.AAC-RBG.mp4` (bare file) | `The Impossible (2012) [1080p]/The Impossible (2012) [1080p].mp4` |
| `A.I. Artificial Intelligence (2001) (1080p BluRay … AAC 6.1 Silence)` | `A.I. Artificial Intelligence (2001) [1080p]/…` |
| `Uptown Girls (2003) [1080p] [YTS.AG]` | `Uptown Girls (2003) [1080p]/…` |
| `Blade.Runner.2049.2017.2160p.UHD…` | `Blade Runner 2049 (2017) [2160p]/…` |
| `Silo.S02.1080p.ATVP.WEB-DL/Silo.S02E01….mkv` | `Silo (2023) [1080p]/Season 02/S02E01 - The Engineer.mkv` |
| `Breaking.Bad.S01E01.720p.BluRay/…` (+ `Sample/`) | `Breaking Bad (2008) [720p]/Season 01/S01E01 - Pilot.mkv`; the sample is skipped and reported |
