# Music

## Library standard

```
<Album Artist>/<Album> (<Year>)/<NN> - <Title>.<ext>
```
- Multi-disc albums use `<D>-<NN> - <Title>.<ext>` in the same album folder.
- `apply-music` builds every name from the tags, so the job is getting the tags right.
- Lyrics sit next to the song as `<same name>.lrc` (synced) or `<same name>.txt` (plain, only when `music.plain_lyrics` is on).
- Cover art is embedded in each file as a single front cover (picture type 3).

The destination is `destinations.music` from the config, or the source folder itself when unset.

## Which path to take

There are two optional features, settled in SKILL.md step 3:
- **Metadata fixing** (`music.metadata`): MusicBrainz tag correction, official covers, junk cleanup. **On** → follow steps 1–4. **Off** → skip to **Rename only**.
- **Lyrics** (`music.lyrics`): step 5. Independent of the metadata choice.

## 1. Inspect
```bash
"<mi>" inspect "<folder>"
```
Group the files by album. For each file, note missing, wrong, or junk fields (see the checklist below).

## 2. Look up the canonical data (MusicBrainz)
**Batches (more than ~5 files):** start with the drafter:
```bash
"<mi>" draft-music "<folder>" "<scratch>/drafts.json"
```
For every song it drafts the release group, canonical tags, and track numbers. It prefers the original studio album or EP, falls back to a single, and finds the track in the earliest official CD or digital release. Then review the printout:
- **Re-check by hand:** any song whose draft picked a compilation (album artist `Various Artists`), any song that matched nothing, and any song tagged to a reissue when the original album exists. Use the manual commands below.
- **Length decides the version:** if the original album's version differs by more than ~3s, the file is a different recording (live, early, alternate). Keep it on the release it actually came from.
- **Replacements:** a song that replaces one already in the library must get that copy's exact tags, so its folder and filename line up and it's detected as an upgrade.

For single files, or to check a draft by hand:
```bash
"<mi>" mb album  --artist "X" --album "Y"
"<mi>" mb detail --rg <release-group-id>
"<mi>" mb track  --artist "X" --title "Y"
```
- `album` lists candidate release groups. Pick the one whose artist matches and whose type is Album, EP, or Single. Prefer the original release over compilations, live albums, and deluxe reissues, unless the file clearly comes from one of those (check the track count and duration).
- `detail` gives the canonical title and artist spelling, the first release date, and the tracklist.
- `track` finds which albums contain a song, for files with a missing or doubtful album tag. Use the song's length to tell versions apart.
- The scripts rate-limit themselves to MusicBrainz's ~1 request per second. **Don't run lookups in parallel.**

## 3. Get the covers
```bash
"<mi>" mb cover --rg <id> --out "<scratch>/<album>.jpg"
```
This fetches the official 1200px front cover from the Cover Art Archive.
- **Look at every cover** (open the image) before using it. Check that it's the right album and the front cover, not a back cover, disc, or different edition. For batches, put the covers side by side in one image if you have a way to, so one look checks many; otherwise open them one by one.
- If the online cover is under ~600px or not square, compare it with the art already embedded in the file, and use whichever is larger if it's the correct front cover.
- If the Cover Art Archive has nothing, keep the file's existing art only if it's the front cover and at least ~500px. Otherwise flag it in the report.

## 4. Write the plan and apply it
Write `<scratch>/plan.json` (the format is in `"<mi>" apply-music -h`). For each file, give every tag you've verified: title, artist, albumartist, album, date (year), track, tracktotal, disc, disctotal, `cover`, and `rg`. Leave genre alone unless it's missing, and never invent one. Set `"strip_embedded_lyrics": true` when lyrics are on and plain lyrics are off, so players only see the synced `.lrc`.
```bash
"<mi>" apply-music "<scratch>/plan.json" --manifest "<scratch>/new_songs.txt"
```
It writes the tags, replaces the embedded art with the single front cover, strips junk and release-specific IDs, moves each song (with any `.lrc`/`.txt`) into the library layout, and removes source folders once they're empty. Use `--dry-run` first if anything about the plan is unusual.

## Rename only (metadata fixing off)
```bash
"<mi>" apply-music --rename-only "<folder>" --manifest "<scratch>/new_songs.txt"
```
Tags are left exactly as they are, and files are moved using their existing album artist (or artist), album, year, title, and track number. Files missing any of those are reported and left in place. Tell the user which ones, and offer to run metadata fixing on just those files.

## 5. Lyrics (optional)
```bash
"<mi>" lyrics --files "<scratch>/new_songs.txt" [--plain]
```
Add `--plain` when `music.plain_lyrics` is true. The script saves the closest synced match within 3 seconds of the song's length as `.lrc`. With `--plain`, it saves unsynced lyrics as `.txt` when no synced version exists. Songs that already have a `.lrc` are skipped. Using the manifest keeps it to this run's songs, which matters when the destination is a whole library. To backfill an entire library later, pass the folder instead of `--files`.

## Conflicts and upgrades
- **CONFLICT:** the exact library path already exists. The song stays in the source folder. Tell the user and ask whether the library copy should be replaced. Don't work around it.
- **UPGRADE:** the same song exists in another format (e.g. a new `.flac` for an old `.mp3`). It's listed, nothing moves, and the script exits with code 2. If the user said they're replacing or upgrading songs, re-run with `--replace`. Otherwise show the list and ask first. `--replace` moves the old file into `<library>/.replaced/` (never deletes it). It keeps the library's `.lrc` when the two versions' lengths match within 2s; otherwise it sets the old `.lrc` aside too so step 5 can fetch a fresh one.
- Upgrades are only detected when the tags produce the same folder and filename. If the old version sits in a differently named folder, point that out rather than leaving a duplicate.

## Duplicates in the source folder
If the same song appears twice, keep the better copy (judged by tags, art, bitrate, identical audio). Move the other to `<source>/.duplicates/`, which every script ignores, and tell the user it's there. Never delete it.

## Metadata checklist

- **Album artist** must be set, because music servers group albums by it. Use the main artist, or `Various Artists` for true compilations. Featured artists go in the track artist field, never in the album artist.
- **Capitalization:** use MusicBrainz's spelling exactly ("Alice in Chains", not "Alice In Chains"), because inconsistent capitalization splits one artist or album into two. Exception: always use straight apostrophes (`'`), not curly ones (`’`).
- **Existing folders:** new songs for an artist or album already in the library merge into the existing folder only if the spelling matches exactly. Check new artist and album names against the destination before applying.
- **Year:** the release group's original first-release year. Remasters and reissues keep the original year unless the file truly is a distinct edition.
- **Track and disc:** always `n/total`. Single-disc albums get `1/1`.
- **Title:** the canonical track title. Drop junk like "(Official Audio)", "[HQ]", or a track number repeated in the title. Keep real version info such as "(Live)" or "(Remastered 2011)" only if the file is that version.
- **Release-specific IDs get stripped** (by `apply-music`): MusicBrainz release/album IDs, release-track IDs, alternate track totals, original/release dates, barcode, catalog number, album ReplayGain. Leftover release IDs from a deluxe or compilation source make servers like Navidrome split one album into several. Recording- and artist-level IDs are kept. To repair an existing library: `"<mi>" release-tags "<library>" --dry-run`, then without `--dry-run`.
- **Junk that gets stripped:** comments ("Cleaned by TuneUp!", ripper and website stamps), encoder tags, iTunes and Windows Media Player data, CD identifiers, stray custom tags, stale ID3v1 tags (MP3s are saved as ID3v2.3, the most widely readable version), and embedded lyrics when `strip_embedded_lyrics` is set. MusicBrainz and ReplayGain tags are kept.
- **Cover:** one embedded front cover. A small thumbnail, a picture not marked as front cover, or several pictures all count as needing replacement.

## Formats
Only **MP3 and FLAC** are handled: tags are read and written for both. Other audio (M4A, OGG, Opus, WMA, WAV...) shows up under `audio_unsupported` in `scan`. Leave it in the source folder and list it in the report.
