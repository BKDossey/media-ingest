package music

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"media-ingest/internal/core"
)

const ApplyDoc = `Usage:
  media-ingest apply-music <plan.json> [--dry-run] [--replace] [--manifest new_songs.txt]
  media-ingest apply-music --rename-only <folder> [--dest DIR] [--dry-run] [--replace] [--manifest FILE]

plan.json:
{
  "root": "/path/to/incoming",            source folder; "src" paths are relative to it
  "dest": "/path/to/Music",               optional; default: config destinations.music, else root
  "strip_embedded_lyrics": true,          optional; default false
  "files": [
    {"src": "02 Nutshell.mp3",
     "tags": {"title": "Nutshell", "artist": "Alice in Chains", "albumartist": "Alice in Chains",
              "album": "Jar of Flies", "date": "1994", "track": 2, "tracktotal": 7,
              "disc": 1, "disctotal": 1, "genre": "Alternative Rock"},
     "cover": "/tmp/jof.jpg",
     "rg": "<MusicBrainz release group id>"}
  ]
}
Only the tag keys given are written. "cover" replaces every embedded picture with one
front cover. "rg" records the chosen MusicBrainz release group. Release-specific tags
(release IDs, alternate track totals, original/release dates, barcode...) and junk
(comments, encoder stamps, iTunes/WMP data, ID3v1) are always stripped. Embedded lyrics
are removed only with "strip_embedded_lyrics": true. MP3s are saved as ID3v2.3.

--rename-only writes no tags at all: every MP3/FLAC under <folder> is moved using the
tags it already has. Files missing a needed tag are reported and left in place.

Result: <dest>/<Album Artist>/<Album> (<Year>)/<NN> - <Title>.<ext>
(multi-disc: "<D>-<NN> - <Title>.<ext>"). .lrc/.txt lyrics sidecars move with their song.

Never overwrites: an existing library path is a CONFLICT and the song stays put. The same
song in the library in the other format (e.g. a new .flac for an old .mp3) is an UPGRADE:
listed and left alone unless --replace is given, which moves the old file to
<dest>/.replaced/ (never deletes it) once the new one is in place.
--manifest writes the final path of every song moved, one per line (for "lyrics --files").
Exit codes: 0 ok, 1 problems, 2 upgrades waiting for --replace.
`

// Plan is apply-music's input.
type Plan struct {
	Root        string     `json:"root"`
	Dest        string     `json:"dest"`
	StripLyrics bool       `json:"strip_embedded_lyrics"`
	Files       []PlanFile `json:"files"`
}

type PlanFile struct {
	Src   string  `json:"src"`
	Tags  NewTags `json:"tags"`
	Cover string  `json:"cover"`
	RG    string  `json:"rg"`
}

const sameLength = 2.0 // seconds: an upgrade this close in length keeps the library's .lrc

// RunApply implements "media-ingest apply-music".
func RunApply(args []string) int {
	fs := core.NewFlags("apply-music", ApplyDoc)
	renameOnly := fs.String("rename-only", "", "")
	destFlag := fs.String("dest", "", "")
	dry := fs.Bool("dry-run", false, "")
	replace := fs.Bool("replace", false, "")
	manifest := fs.String("manifest", "", "")
	pos, ok := core.Parse(fs, args)
	if !ok {
		return 2
	}
	var plan Plan
	switch {
	case *renameOnly != "":
		root, _ := filepath.Abs(core.Expand(*renameOnly))
		plan.Root = root
		for _, f := range core.Walk(root, core.TaggableAudio) {
			plan.Files = append(plan.Files, PlanFile{Src: core.Rel(root, f)})
		}
	case len(pos) == 1:
		if err := core.ReadJSON(pos[0], &plan); err != nil {
			return core.Fail("%v", err)
		}
		plan.Root = core.Expand(plan.Root)
	default:
		fs.Usage()
		return 2
	}
	cfg, err := core.LoadConfig()
	if err != nil {
		return core.Fail("%v", err)
	}
	dest := plan.Root
	if *destFlag != "" {
		dest = core.Expand(*destFlag)
	} else if plan.Dest != "" {
		dest = core.Expand(plan.Dest)
	} else if cfg.Destinations.Music != nil && *cfg.Destinations.Music != "" {
		dest = core.Expand(*cfg.Destinations.Music)
	}

	errors := 0
	var moved, upgrades []string
	touched := map[string]bool{}
	for _, item := range plan.Files {
		src := filepath.Join(plan.Root, filepath.FromSlash(item.Src))
		ext := core.Ext(src)
		if !core.Exists(src) {
			fmt.Println("MISSING: " + item.Src)
			errors++
			continue
		}
		if !core.TaggableAudio[ext] {
			fmt.Printf("SKIP (unsupported format %s; only MP3 and FLAC are handled): %s\n", ext, item.Src)
			errors++
			continue
		}
		writing := *renameOnly == "" && (!item.Tags.Empty() || item.Cover != "" || item.RG != "")
		var cover []byte
		if item.Cover != "" {
			if cover, err = os.ReadFile(core.Expand(item.Cover)); err != nil {
				fmt.Printf("COVER UNREADABLE (%v): %s\n", err, item.Src)
				errors++
				continue
			}
		}
		if writing && !*dry {
			if err := WriteTags(src, item.Tags, WriteOptions{Cover: cover, ReleaseGroup: item.RG, StripLyrics: plan.StripLyrics}); err != nil {
				fmt.Printf("TAG WRITE FAILED (%v): %s\n", err, item.Src)
				errors++
				continue
			}
		}
		s, err := Read(src)
		if err != nil {
			fmt.Printf("UNREADABLE (%v): %s\n", err, item.Src)
			errors++
			continue
		}
		t := s.Tags
		if writing && *dry { // show what the plan would produce
			overlay(&t, item.Tags)
		}
		albumArtist := t.AlbumArtist
		if albumArtist == "" {
			albumArtist = t.Artist
		}
		year := t.Date
		if len(year) > 4 {
			year = year[:4]
		}
		var missing []string
		for _, f := range []struct{ name, val string }{{"album artist", albumArtist}, {"album", t.Album},
			{"year", year}, {"title", t.Title}} {
			if f.val == "" {
				missing = append(missing, f.name)
			}
		}
		if t.Track == 0 {
			missing = append(missing, "track number")
		}
		if len(missing) > 0 {
			fmt.Printf("NOT MOVED (missing %s): %s\n", strings.Join(missing, ", "), item.Src)
			errors++
			continue
		}
		disc, discTotal := t.Disc, t.DiscTotal
		if disc == 0 {
			disc = 1
		}
		prefix := fmt.Sprintf("%02d", t.Track)
		if discTotal > 1 {
			prefix = fmt.Sprintf("%d-%02d", disc, t.Track)
		}
		rel := filepath.Join(core.SafeName(albumArtist), core.SafeName(fmt.Sprintf("%s (%s)", t.Album, year)),
			fmt.Sprintf("%s - %s%s", prefix, core.SafeName(t.Title), filepath.Ext(src)))
		target := filepath.Join(dest, rel)

		if core.Exists(target) && target != src && !core.SameFile(target, src) {
			fmt.Println("CONFLICT, already in library: " + rel)
			errors++
			continue
		}
		var others []string
		for _, e := range []string{".mp3", ".flac"} {
			if e != ext && core.Exists(core.WithExt(target, e)) {
				others = append(others, core.WithExt(target, e))
			}
		}
		if len(others) > 0 {
			var names []string
			for _, o := range others {
				names = append(names, filepath.Base(o))
			}
			if !*replace {
				upgrades = append(upgrades, fmt.Sprintf("%s  would replace  %s", item.Src, strings.Join(names, ", ")))
				continue
			}
			if *dry {
				fmt.Printf("WOULD UPGRADE: %s replaces %s\n", rel, strings.Join(names, ", "))
				continue
			}
			backup := filepath.Join(dest, ".replaced", filepath.Dir(rel))
			oldLen := 0.0
			if old, err := Read(others[0]); err == nil {
				oldLen = old.Duration
			}
			for _, o := range others {
				if err := core.MoveFile(o, filepath.Join(backup, filepath.Base(o))); err != nil {
					fmt.Printf("FAILED to set aside %s: %v\n", o, err)
					errors++
				}
			}
			oldLrc, newLrc := core.WithExt(target, ".lrc"), core.WithExt(src, ".lrc")
			if core.Exists(oldLrc) && (core.Exists(newLrc) || oldLen == 0 || math.Abs(oldLen-s.Duration) > sameLength) {
				core.MoveFile(oldLrc, filepath.Join(backup, filepath.Base(oldLrc)))
				if !core.Exists(newLrc) {
					fmt.Printf("  note: lengths differ, old .lrc set aside; fetch new lyrics for %s\n", rel)
				}
			}
			fmt.Printf("UPGRADE: %s replaces %s (old file kept in %s)\n", rel, strings.Join(names, ", "), backup)
		}

		if *dry {
			fmt.Printf("WOULD MOVE: %s  ->  %s\n", item.Src, rel)
			continue
		}
		for _, side := range []string{".lrc", ".txt"} {
			if sc := core.WithExt(src, side); core.Exists(sc) && !core.Exists(core.WithExt(target, side)) {
				core.MoveFile(sc, core.WithExt(target, side))
			}
		}
		if err := core.MoveFile(src, target); err != nil {
			fmt.Printf("FAILED (%v): %s\n", err, item.Src)
			errors++
			continue
		}
		touched[filepath.Dir(src)] = true
		moved = append(moved, target)
		fmt.Printf("OK: %s  ->  %s\n", item.Src, rel)
	}

	if len(upgrades) > 0 {
		fmt.Println("\nUPGRADES (same song already in the library in another format). Not moved;")
		fmt.Println("re-run with --replace to swap them in (old files are set aside in .replaced/, not deleted):")
		for _, u := range upgrades {
			fmt.Println("  " + u)
		}
	}
	if !*dry && len(touched) > 0 {
		var dirs []string
		for d := range touched {
			dirs = append(dirs, d)
		}
		left := core.RemoveEmptyDirs(dirs, plan.Root)
		var keys []string
		for k := range left {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("left behind (not empty): %s: %s\n", k, strings.Join(left[k], ", "))
		}
	}
	if *manifest != "" && !*dry {
		body := strings.Join(moved, "\n")
		if len(moved) > 0 {
			body += "\n"
		}
		if err := os.WriteFile(*manifest, []byte(body), 0o644); err != nil {
			return core.Fail("%v", err)
		}
		fmt.Printf("manifest: %d file(s) -> %s\n", len(moved), *manifest)
	}
	fmt.Printf("\n%d moved, %d upgrade(s) waiting, %d problem(s)  (library: %s)\n", len(moved), len(upgrades), errors, dest)
	switch {
	case errors > 0:
		return 1
	case len(upgrades) > 0:
		return 2
	}
	return 0
}

func overlay(t *Tags, n NewTags) {
	str := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	num := func(dst *int, v *FlexInt) {
		if v != nil {
			*dst = int(*v)
		}
	}
	str(&t.Title, n.Title)
	str(&t.Artist, n.Artist)
	str(&t.AlbumArtist, n.AlbumArtist)
	str(&t.Album, n.Album)
	if n.Date != nil {
		t.Date = string(*n.Date)
	}
	num(&t.Track, n.Track)
	num(&t.TrackTotal, n.TrackTotal)
	num(&t.Disc, n.Disc)
	num(&t.DiscTotal, n.DiscTotal)
}
