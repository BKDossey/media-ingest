package music

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"media-ingest/internal/core"
)

const DraftDoc = `Usage: media-ingest draft-music <folder> <out.json>

Drafts canonical MusicBrainz data for every MP3/FLAC under <folder>, for review before
apply-music. For each song it picks the release group (preferring the original studio
album/EP over singles, compilations, live albums and reissues), finds the track in that
group's earliest official CD/digital release, and records canonical title, artists, year,
and track/disc numbers. Every draft carries "notes" on shaky matches. Review them; this is
a draft, not a decision.
`

const lenTol = 4.0 // seconds

var goodFormats = map[string]bool{"CD": true, "Digital Media": true, "Enhanced CD": true, "HDCD": true, "SACD": true}

var (
	featParen = regexp.MustCompile(`[\(\[][^)\]]*\b(feat|ft|with|featuring|cover)\b[^)\]]*[\)\]]`)
	featTail  = regexp.MustCompile(`\b(feat|ft|featuring)\b.*`)
	notAlnum  = regexp.MustCompile(`[^a-z0-9]`)
	artistSep = regexp.MustCompile(`\s*(?:;|,| feat\.? | ft\.? | & | x )\s*`)
	bracketed = regexp.MustCompile(`[(\[].*?[)\]]`)
)

// normTitle compares titles loosely: no accents, featured artists, or punctuation.
func normTitle(s string) string {
	s = strings.ToLower(strings.ReplaceAll(norm.NFKD.String(s), "’", "'"))
	s = featParen.ReplaceAllString(s, "")
	s = featTail.ReplaceAllString(s, "")
	return notAlnum.ReplaceAllString(s, "")
}

// DraftEntry is one song in draft-music's output.
type DraftEntry struct {
	Src    string         `json:"src"`
	RG     string         `json:"rg,omitempty"`
	RGType string         `json:"rg_type,omitempty"`
	Tags   *DraftTags     `json:"tags,omitempty"`
	File   map[string]any `json:"file"`
	Notes  []string       `json:"notes"`
}

// DraftTags uses the same keys as a plan's "tags", so it can be copied straight in.
type DraftTags struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	AlbumArtist string `json:"albumartist"`
	Album       string `json:"album"`
	Date        string `json:"date"`
	Track       int    `json:"track"`
	TrackTotal  int    `json:"tracktotal"`
	Disc        int    `json:"disc"`
	DiscTotal   int    `json:"disctotal"`
}

type trackMatch struct {
	tags   DraftTags
	length float64
}

func isStudio(g *releaseGroup) bool {
	return g != nil && (g.PrimaryType == "Album" || g.PrimaryType == "EP") && len(g.SecondaryTypes) == 0
}

// findTrack looks for title (at ~dur) in the earliest official CD/digital release of rg.
func findTrack(rg, title string, dur float64) *trackMatch {
	type hit struct {
		unofficial, badFormat bool
		date                  string
		discs                 int
		rel                   release
		med                   medium
		tr                    mbTrack
	}
	var hits []hit
	for _, rel := range groupReleases(rg) {
		good := true
		for _, m := range rel.Media {
			good = good && goodFormats[m.Format]
		}
		for _, m := range rel.Media {
			for _, t := range m.Tracks {
				tl := float64(t.Length) / 1000
				if normTitle(t.Title) == normTitle(title) && (tl == 0 || math.Abs(tl-dur) <= lenTol) {
					hits = append(hits, hit{rel.Status != "Official", !good, dateOr(rel.Date), len(rel.Media), rel, m, t})
				}
			}
		}
	}
	if len(hits) == 0 {
		return nil
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.unofficial != b.unofficial {
			return !a.unofficial
		}
		if a.badFormat != b.badFormat {
			return !a.badFormat
		}
		if a.date != b.date {
			return a.date < b.date
		}
		return a.discs < b.discs
	})
	h := hits[0]
	artist := h.tr.ArtistCredit.String()
	if artist == "" {
		artist = h.rel.ArtistCredit.String()
	}
	pos, _ := h.tr.Position.Int64()
	total := h.med.TrackCount
	if total == 0 {
		total = len(h.med.Tracks)
	}
	disc := h.med.Position
	if disc == 0 {
		disc = 1
	}
	return &trackMatch{DraftTags{Title: h.tr.Title, Artist: artist, Track: int(pos), TrackTotal: total,
		Disc: disc, DiscTotal: len(h.rel.Media)}, math.Round(float64(h.tr.Length)/100) / 10}
}

type rgCand struct {
	id, date, ptype string
	secondary       []string
}

// recordingGroups lists release groups containing artist/title near dur, best first.
func recordingGroups(artist, title string, dur float64) []rgCand {
	seen := map[string]rgCand{}
	for _, rec := range searchRecordings(artist, title, 40) {
		rl := float64(rec.Length) / 1000
		if normTitle(rec.Title) != normTitle(title) || (rl > 0 && math.Abs(rl-dur) > lenTol) {
			continue
		}
		for _, rel := range rec.Releases {
			g := rel.ReleaseGroup
			if g.ID == "" {
				continue
			}
			d := dateOr(rel.Date)
			if prev, ok := seen[g.ID]; !ok || d < prev.date {
				seen[g.ID] = rgCand{g.ID, d, g.PrimaryType, g.SecondaryTypes}
			}
		}
	}
	var out []rgCand
	for _, c := range seen {
		out = append(out, c)
	}
	rank := func(c rgCand) int {
		switch {
		case (c.ptype == "Album" || c.ptype == "EP") && len(c.secondary) == 0:
			return 0
		case c.ptype == "Single" && len(c.secondary) == 0:
			return 1
		}
		return 2
	}
	sort.Slice(out, func(i, j int) bool {
		if rank(out[i]) != rank(out[j]) {
			return rank(out[i]) < rank(out[j])
		}
		if out[i].date != out[j].date {
			return out[i].date < out[j].date
		}
		return out[i].id < out[j].id
	})
	return out
}

// RunDraft implements "media-ingest draft-music".
func RunDraft(args []string) int {
	fs := core.NewFlags("draft-music", DraftDoc)
	pos, ok := core.Parse(fs, args)
	if !ok || len(pos) != 2 {
		fs.Usage()
		return 2
	}
	root, _ := filepath.Abs(core.Expand(pos[0]))
	drafts := []DraftEntry{}
	for _, f := range core.Walk(root, core.TaggableAudio) {
		rel := core.Rel(root, f)
		s, err := Read(f)
		if err != nil {
			drafts = append(drafts, DraftEntry{Src: rel, File: map[string]any{}, Notes: []string{"unreadable: " + err.Error()}})
			fmt.Printf("!! %s: unreadable (%v)\n", filepath.Base(f), err)
			continue
		}
		t := s.Tags
		file := map[string]any{"artist": t.Artist, "albumartist": t.AlbumArtist, "album": t.Album,
			"title": t.Title, "duration": math.Round(s.Duration*100) / 100, "rg": t.ReleaseGroup}
		e := DraftEntry{Src: rel, File: file, Notes: []string{}}

		artist := t.Artist
		if t.AlbumArtist != "" && !strings.Contains(strings.ToLower(t.AlbumArtist), "various") {
			artist = t.AlbumArtist
		}
		if a := artistSep.Split(artist, 2)[0]; a != "" {
			artist = a
		} else {
			artist = t.Artist
		}

		var chosen string
		var match *trackMatch
		// 1. the file's own release group, if it's a studio album/EP containing the song
		if t.ReleaseGroup != "" {
			g := getReleaseGroup(t.ReleaseGroup)
			if isStudio(g) {
				if match = findTrack(t.ReleaseGroup, t.Title, s.Duration); match != nil {
					chosen = t.ReleaseGroup
				} else {
					e.Notes = append(e.Notes, "own release group lacks this track")
				}
			} else if g != nil {
				e.Notes = append(e.Notes, fmt.Sprintf("file tagged to %s %v '%s'", g.PrimaryType, g.SecondaryTypes, g.Title))
			}
		}
		// 2. album search by name
		if chosen == "" && t.Album != "" {
			bare := strings.TrimSpace(bracketed.ReplaceAllString(t.Album, ""))
			for _, g := range searchReleaseGroups(artist, bare, 5) {
				if g.Score >= 90 && isStudio(&g) {
					if match = findTrack(g.ID, t.Title, s.Duration); match != nil {
						chosen = g.ID
						break
					}
				}
			}
		}
		// 3. which albums contain this recording
		if chosen == "" && t.Title != "" {
			cands := recordingGroups(artist, t.Title, s.Duration)
			if len(cands) > 6 {
				cands = cands[:6]
			}
			for _, c := range cands {
				if match = findTrack(c.id, t.Title, s.Duration); match != nil {
					chosen = c.id
					if c.ptype == "Single" {
						e.Notes = append(e.Notes, "only found on a single")
					}
					break
				}
			}
		}
		if chosen == "" {
			e.Notes = append(e.Notes, "NO MATCH: review by hand")
			drafts = append(drafts, e)
			fmt.Printf("!! %s: no match\n", filepath.Base(f))
			continue
		}
		g := getReleaseGroup(chosen)
		tags := match.tags
		tags.AlbumArtist, tags.Album = g.ArtistCredit.String(), g.Title
		if len(g.FirstRelease) >= 4 {
			tags.Date = g.FirstRelease[:4]
		}
		if normTitle(tags.Title) != normTitle(t.Title) {
			e.Notes = append(e.Notes, fmt.Sprintf("title differs from file: '%s'", t.Title))
		}
		if match.length > 0 && math.Abs(match.length-s.Duration) > 2 {
			e.Notes = append(e.Notes, fmt.Sprintf("length %.1fs vs MB %.1fs", s.Duration, match.length))
		}
		e.RG, e.RGType, e.Tags = chosen, g.PrimaryType, &tags
		drafts = append(drafts, e)
		name := filepath.Base(f)
		if len(name) > 42 {
			name = name[:42]
		}
		fmt.Printf("%-42s -> %s / %s (%s) %d-%02d/%d '%s' [%s] %s\n", name, tags.AlbumArtist, tags.Album, tags.Date,
			tags.Disc, tags.Track, tags.TrackTotal, tags.Title, tags.Artist, strings.Join(e.Notes, "; "))
	}
	if err := core.WriteJSON(pos[1], drafts); err != nil {
		return core.Fail("%v", err)
	}
	fmt.Printf("\n%d drafts -> %s\n", len(drafts), pos[1])
	return 0
}
