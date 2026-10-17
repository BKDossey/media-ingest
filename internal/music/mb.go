package music

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"media-ingest/internal/core"
)

const mbAPI = "https://musicbrainz.org/ws/2/"

// MusicBrainz allows about one request per second.
var lastMB time.Time

func mb(path string, params url.Values, v any) error {
	if wait := 1100*time.Millisecond - time.Since(lastMB); wait > 0 {
		time.Sleep(wait)
	}
	defer func() { lastMB = time.Now() }()
	if params == nil {
		params = url.Values{}
	}
	params.Set("fmt", "json")
	return core.GetJSON(mbAPI+path, params, v)
}

type artistCredit []struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
}

func (ac artistCredit) String() string {
	var b strings.Builder
	for _, a := range ac {
		b.WriteString(a.Name + a.JoinPhrase)
	}
	return b.String()
}

type releaseGroup struct {
	ID             string       `json:"id"`
	Title          string       `json:"title"`
	PrimaryType    string       `json:"primary-type"`
	SecondaryTypes []string     `json:"secondary-types"`
	FirstRelease   string       `json:"first-release-date"`
	ArtistCredit   artistCredit `json:"artist-credit"`
	Score          int          `json:"score"`
}

type mbTrack struct {
	Position     json.Number  `json:"position"`
	Title        string       `json:"title"`
	Length       int          `json:"length"`
	ArtistCredit artistCredit `json:"artist-credit"`
}

type medium struct {
	Position   int       `json:"position"`
	Format     string    `json:"format"`
	TrackCount int       `json:"track-count"`
	Tracks     []mbTrack `json:"tracks"`
}

type release struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Status       string       `json:"status"`
	Date         string       `json:"date"`
	Country      string       `json:"country"`
	Media        []medium     `json:"media"`
	ArtistCredit artistCredit `json:"artist-credit"`
	ReleaseGroup releaseGroup `json:"release-group"`
}

type recording struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Length       int          `json:"length"`
	ArtistCredit artistCredit `json:"artist-credit"`
	Releases     []release    `json:"releases"`
}

func esc(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }

func searchReleaseGroups(artist, album string, limit int) []releaseGroup {
	var res struct {
		Groups []releaseGroup `json:"release-groups"`
	}
	q := fmt.Sprintf(`releasegroup:"%s" AND artist:"%s"`, esc(album), esc(artist))
	mb("release-group", url.Values{"query": {q}, "limit": {fmt.Sprint(limit)}}, &res)
	return res.Groups
}

func searchRecordings(artist, title string, limit int) []recording {
	var res struct {
		Recordings []recording `json:"recordings"`
	}
	q := fmt.Sprintf(`recording:"%s" AND artist:"%s"`, esc(title), esc(artist))
	mb("recording", url.Values{"query": {q}, "limit": {fmt.Sprint(limit)}}, &res)
	return res.Recordings
}

var rgCache = map[string]*releaseGroup{}
var relCache = map[string][]release{}

func getReleaseGroup(id string) *releaseGroup {
	if g, ok := rgCache[id]; ok {
		return g
	}
	var g releaseGroup
	if err := mb("release-group/"+id, url.Values{"inc": {"artist-credits"}}, &g); err != nil {
		rgCache[id] = nil
		return nil
	}
	rgCache[id] = &g
	return &g
}

// groupReleases lists a release group's releases with tracklists (up to 100).
func groupReleases(rg string) []release {
	if r, ok := relCache[rg]; ok {
		return r
	}
	var all []release
	for offset := 0; offset < 100; offset += 25 {
		var page struct {
			Releases []release `json:"releases"`
			Count    int       `json:"release-count"`
		}
		if err := mb("release", url.Values{"release-group": {rg}, "inc": {"recordings+media+artist-credits"},
			"limit": {"25"}, "offset": {fmt.Sprint(offset)}}, &page); err != nil {
			break
		}
		all = append(all, page.Releases...)
		if offset+25 >= page.Count {
			break
		}
	}
	relCache[rg] = all
	return all
}

const mbDoc = `Usage:
  media-ingest mb album  --artist "Linkin Park" --album "Meteora"
  media-ingest mb track  --artist "Linkin Park" --title "Somewhere I Belong"
  media-ingest mb detail --rg <release-group-id>
  media-ingest mb cover  --rg <release-group-id> --out <file.jpg>

album  : candidate release groups (id, canonical title/artist, type, first release date, score)
track  : which albums a song appears on, for files with a missing/wrong album tag
detail : canonical data plus the tracklist of the earliest official release
cover  : downloads the release group's 1200px front cover from the Cover Art Archive
`

// RunMB implements "media-ingest mb".
func RunMB(args []string) int {
	fs := core.NewFlags("mb", mbDoc)
	artist := fs.String("artist", "", "")
	album := fs.String("album", "", "")
	title := fs.String("title", "", "")
	rg := fs.String("rg", "", "")
	out := fs.String("out", "", "")
	pos, ok := core.Parse(fs, args)
	if !ok || len(pos) != 1 {
		fs.Usage()
		return 2
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	switch pos[0] {
	case "album":
		if *artist == "" || *album == "" {
			fs.Usage()
			return 2
		}
		for _, g := range searchReleaseGroups(*artist, *album, 8) {
			enc.Encode(map[string]any{"rg": g.ID, "title": g.Title, "artist": g.ArtistCredit.String(),
				"type": g.PrimaryType, "secondary": nonNil(g.SecondaryTypes), "first_release": g.FirstRelease, "score": g.Score})
		}
	case "track":
		if *artist == "" || *title == "" {
			fs.Usage()
			return 2
		}
		seen := map[string]bool{}
		for _, rec := range searchRecordings(*artist, *title, 15) {
			for _, rel := range rec.Releases {
				g := rel.ReleaseGroup
				if g.ID == "" || seen[g.ID] {
					continue
				}
				seen[g.ID] = true
				enc.Encode(map[string]any{"rg": g.ID, "album": g.Title, "type": g.PrimaryType,
					"secondary": nonNil(g.SecondaryTypes), "date": rel.Date, "recording": rec.Title,
					"artist": rec.ArtistCredit.String(), "length_s": (rec.Length + 500) / 1000})
			}
		}
	case "detail":
		if *rg == "" {
			fs.Usage()
			return 2
		}
		g := getReleaseGroup(*rg)
		if g == nil {
			return core.Fail("release group %s not found", *rg)
		}
		rels := groupReleases(*rg)
		var official []release
		for _, r := range rels {
			if r.Status == "Official" {
				official = append(official, r)
			}
		}
		if len(official) == 0 {
			official = rels
		}
		sort.SliceStable(official, func(i, j int) bool {
			di, dj := dateOr(official[i].Date), dateOr(official[j].Date)
			if di != dj {
				return di < dj
			}
			return len(official[i].Media) > len(official[j].Media)
		})
		type tr struct {
			N       string `json:"n"`
			Title   string `json:"title"`
			Seconds int    `json:"length_s"`
		}
		type disc struct {
			Disc   int    `json:"disc"`
			Format string `json:"format"`
			Tracks []tr   `json:"tracks"`
		}
		res := map[string]any{"rg": *rg, "title": g.Title, "artist": g.ArtistCredit.String(),
			"type": g.PrimaryType, "first_release": g.FirstRelease, "release_used": nil, "discs": []disc{}}
		if len(official) > 0 {
			first := official[0]
			res["release_used"] = map[string]string{"id": first.ID, "date": first.Date, "country": first.Country}
			var discs []disc
			for _, m := range first.Media {
				d := disc{Disc: m.Position, Format: m.Format, Tracks: []tr{}}
				for _, t := range m.Tracks {
					d.Tracks = append(d.Tracks, tr{t.Position.String(), t.Title, (t.Length + 500) / 1000})
				}
				discs = append(discs, d)
			}
			res["discs"] = discs
		}
		core.PrintJSON(res)
	case "cover":
		if *rg == "" || *out == "" {
			fs.Usage()
			return 2
		}
		data, err := core.Get("https://coverartarchive.org/release-group/"+*rg+"/front-1200", nil, 3)
		if err == core.ErrNotFound {
			fmt.Println("NO COVER on Cover Art Archive for this release group")
			return 1
		}
		if err != nil {
			return core.Fail("%v", err)
		}
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			return core.Fail("%v", err)
		}
		fmt.Printf("saved %dB -> %s\n", len(data), *out)
	default:
		fs.Usage()
		return 2
	}
	return 0
}

func dateOr(d string) string {
	if d == "" {
		return "9999"
	}
	return d
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
