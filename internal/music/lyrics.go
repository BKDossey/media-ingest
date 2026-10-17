package music

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"media-ingest/internal/core"
)

const LyricsDoc = `Usage:
  media-ingest lyrics <folder> [--plain]          every song under the folder
  media-ingest lyrics --files <list.txt> [--plain] only the songs listed, one path per line
                                                    (e.g. apply-music's --manifest)

Fetches lyrics from LRCLIB (free, no API key). Synced lyrics are saved as "<song>.lrc",
picking the closest match within 3 seconds of the song's length. With --plain, unsynced
lyrics are saved as "<song>.txt" when no synced version exists. Songs that already have a
.lrc are skipped, and a .txt is replaced by a .lrc once a synced version is found.
`

const maxDrift = 3.0 // seconds a candidate's duration may differ from the file

type lrcCandidate struct {
	Duration     float64 `json:"duration"`
	AlbumName    string  `json:"albumName"`
	SyncedLyrics string  `json:"syncedLyrics"`
	PlainLyrics  string  `json:"plainLyrics"`
	Instrumental bool    `json:"instrumental"`
}

func lrclib(path string, params url.Values) []lrcCandidate {
	body, err := core.Get("https://lrclib.net/api/"+path, params, 5)
	if err != nil {
		return nil
	}
	var many []lrcCandidate
	if json.Unmarshal(body, &many) == nil {
		return many
	}
	var one lrcCandidate
	if json.Unmarshal(body, &one) == nil {
		return []lrcCandidate{one}
	}
	return nil
}

// bestLyrics returns synced and plain lyrics from the closest LRCLIB candidates.
func bestLyrics(artist, title, album string, dur float64) (synced, plain string, instrumental bool) {
	exact := lrclib("get", url.Values{"artist_name": {artist}, "track_name": {title}, "album_name": {album},
		"duration": {fmt.Sprint(math.Round(dur))}})
	cands := append([]lrcCandidate{}, exact...)
	// LRCLIB often has several uploads of a song; the exact match may be a plain-only
	// duplicate while another upload carries synced lyrics.
	if len(exact) == 0 || exact[0].SyncedLyrics == "" {
		cands = append(cands, lrclib("search", url.Values{"artist_name": {artist}, "track_name": {title}})...)
		cands = append(cands, lrclib("search", url.Values{"q": {artist + " " + title}})...)
	}
	var close []lrcCandidate
	for _, c := range cands {
		if c.Duration > 0 && math.Abs(c.Duration-dur) <= maxDrift {
			close = append(close, c)
		}
	}
	sort.SliceStable(close, func(i, j int) bool {
		di, dj := math.Abs(close[i].Duration-dur), math.Abs(close[j].Duration-dur)
		if di != dj {
			return di < dj
		}
		return strings.EqualFold(close[i].AlbumName, album) && !strings.EqualFold(close[j].AlbumName, album)
	})
	for _, c := range close {
		if synced == "" && c.SyncedLyrics != "" {
			synced = c.SyncedLyrics
		}
		if plain == "" && c.PlainLyrics != "" {
			plain = c.PlainLyrics
		}
	}
	return synced, plain, len(exact) > 0 && exact[0].Instrumental
}

// RunLyrics implements "media-ingest lyrics".
func RunLyrics(args []string) int {
	fs := core.NewFlags("lyrics", LyricsDoc)
	files := fs.String("files", "", "")
	plainOK := fs.Bool("plain", false, "")
	pos, ok := core.Parse(fs, args)
	if !ok {
		return 2
	}
	var songs []string
	base := ""
	switch {
	case *files != "":
		b, err := os.ReadFile(*files)
		if err != nil {
			return core.Fail("%v", err)
		}
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" && core.TaggableAudio[core.Ext(l)] {
				songs = append(songs, l)
			}
		}
	case len(pos) == 1:
		base, _ = filepath.Abs(core.Expand(pos[0]))
		songs = core.Walk(base, core.TaggableAudio)
	default:
		fs.Usage()
		return 2
	}

	counts := map[string]int{}
	order := []string{"synced", "upgraded", "plain", "instrumental", "none", "skipped"}
	for _, f := range songs {
		rel := f
		if base != "" {
			rel = core.Rel(base, f)
		}
		lrc, txt := core.WithExt(f, ".lrc"), core.WithExt(f, ".txt")
		if core.Exists(lrc) {
			counts["skipped"]++
			continue
		}
		s, err := Read(f)
		if err != nil || s.Tags.Artist == "" || s.Tags.Title == "" {
			fmt.Println("NO TAGS   " + rel)
			counts["none"]++
			continue
		}
		synced, plain, instrumental := bestLyrics(s.Tags.Artist, s.Tags.Title, s.Tags.Album, s.Duration)
		switch {
		case synced != "":
			if err := os.WriteFile(lrc, []byte(synced), 0o644); err != nil {
				return core.Fail("%v", err)
			}
			if core.Exists(txt) {
				os.Remove(txt)
				fmt.Println("UPGRADED  " + rel + "  (.txt replaced by synced .lrc)")
				counts["upgraded"]++
			} else {
				fmt.Println("SYNCED    " + rel)
				counts["synced"]++
			}
		case instrumental:
			fmt.Println("INSTRUM.  " + rel)
			counts["instrumental"]++
		case plain != "" && *plainOK && !core.Exists(txt):
			if err := os.WriteFile(txt, []byte(plain), 0o644); err != nil {
				return core.Fail("%v", err)
			}
			fmt.Println("PLAIN     " + rel)
			counts["plain"]++
		default:
			why := "no lyrics found"
			if plain != "" && !*plainOK {
				why = "plain only, not saved"
			}
			fmt.Printf("NONE      %s  (%s)\n", rel, why)
			counts["none"]++
		}
		time.Sleep(300 * time.Millisecond)
	}
	var parts []string
	for _, k := range order {
		parts = append(parts, fmt.Sprintf("%s: %d", k, counts[k]))
	}
	fmt.Println("\n" + strings.Join(parts, ", "))
	return 0
}
