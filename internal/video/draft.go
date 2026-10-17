package video

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"media-ingest/internal/core"
)

const DraftDoc = `Usage: media-ingest draft-video <folder> <out.json> [--episode-titles] [--movies-dest DIR] [--tv-dest DIR]

Drafts library names for every movie and TV episode under <folder>. It parses title, year,
quality and season/episode from file and folder names, picks up companion files
(subtitles, .nfo), and proposes each library path. --episode-titles looks every series up
on TVmaze for the canonical name, first-air year and episode titles. Destinations default
to the config file, then to <folder> itself (organize in place). Sample/trailer clips are
listed under "skipped". Read-only: nothing is renamed.

Review the printout, edit the fields in out.json (title, year, quality, season, episode,
episode_title, or "skip": true), then run organize-video on it.
`

// Draft is the JSON written by draft-video and read by organize-video.
type Draft struct {
	Root    string            `json:"root"`
	Dest    map[string]string `json:"dest"`
	Items   []Item            `json:"items"`
	Skipped []Skipped         `json:"skipped"`
}

type Skipped struct {
	Src    string `json:"src"`
	Reason string `json:"reason"`
}

var sample = regexp.MustCompile(`(?i)(^|[\s._\-\[(])(sample|trailer)([\s._\-\])]|$)`)

func probeWidth(path string) int {
	ff, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0
	}
	cmd := exec.Command(ff, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width", "-of", "csv=p=0", path)
	done := make(chan []byte, 1)
	go func() { out, _ := cmd.Output(); done <- out }()
	select {
	case out := <-done:
		w, _ := strconv.Atoi(strings.TrimSpace(strings.Split(string(out), ",")[0]))
		return w
	case <-time.After(30 * time.Second):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return 0
	}
}

func ip(n int) *int { return &n }

// RunDraft implements "media-ingest draft-video".
func RunDraft(args []string) int {
	fs := core.NewFlags("draft-video", DraftDoc)
	titles := fs.Bool("episode-titles", false, "")
	moviesDest := fs.String("movies-dest", "", "")
	tvDest := fs.String("tv-dest", "", "")
	pos, ok := core.Parse(fs, args)
	if !ok || len(pos) != 2 {
		fs.Usage()
		return 2
	}
	root, _ := filepath.Abs(core.Expand(pos[0]))
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return core.Fail("not a folder: %s", root)
	}
	cfg, err := core.LoadConfig()
	if err != nil {
		return core.Fail("%v", err)
	}
	d := Draft{Root: root, Dest: map[string]string{
		"movies": core.Pick(*moviesDest, cfg.Destinations.Movies, root),
		"tv":     core.Pick(*tvDest, cfg.Destinations.TV, root),
	}, Items: []Item{}, Skipped: []Skipped{}}

	var videos []string
	perTop := map[string][]string{}
	for _, f := range core.Walk(root, core.VideoExts) {
		rel := core.Rel(root, f)
		isSample := false
		for _, part := range strings.Split(rel, "/") {
			isSample = isSample || sample.MatchString(part)
		}
		if isSample {
			d.Skipped = append(d.Skipped, Skipped{rel, "sample/trailer clip"})
			continue
		}
		videos = append(videos, f)
		top := strings.Split(rel, "/")[0]
		perTop[top] = append(perTop[top], f)
	}

	claimed := map[string]bool{}
	for _, f := range videos {
		rel := core.Rel(root, f)
		parts := strings.Split(rel, "/")
		inFolder := len(parts) > 1
		top := parts[0]
		lone := inFolder && len(perTop[top]) == 1
		fi := Parse(core.Stem(f))
		var folders []Info
		for i := len(parts) - 2; i >= 0; i-- {
			folders = append(folders, Parse(parts[i]))
		}
		var fo *Info
		for i := range folders {
			if folders[i].Title != "" {
				fo = &folders[i]
				break
			}
		}
		it := Item{Src: rel, Type: "movie", Companions: []Companion{}, Notes: []string{}}

		if fi.Kind == "tv" {
			it.Type = "tv"
			it.Title, it.Year = fi.Title, fi.Year
			if it.Title == "" && fo != nil {
				it.Title = fo.Title
			}
			for _, x := range folders {
				if it.Year == 0 && x.Year > 0 {
					it.Year = x.Year
				}
			}
			it.Season, it.Episode = ip(fi.Season), ip(fi.Episode)
			if fi.EpisodeEnd > 0 {
				it.EpisodeEnd = ip(fi.EpisodeEnd)
			}
			it.EpisodeTitle = fi.EpisodeTitle
		} else if fo != nil && lone {
			it.Title, it.Year = fo.Title, fo.Year
			if it.Year == 0 {
				it.Year = fi.Year
			}
			if fi.Title != "" && !strings.EqualFold(fi.Title, fo.Title) {
				it.Notes = append(it.Notes, fmt.Sprintf("file name says '%s'", fi.Title))
			}
		} else {
			it.Title, it.Year = fi.Title, fi.Year
			if fo != nil {
				if it.Title == "" {
					it.Title = fo.Title
				}
				if it.Year == 0 {
					it.Year = fo.Year
				}
			}
			if inFolder {
				it.Notes = append(it.Notes, "folder holds several videos; parsed from the file name")
			}
		}
		it.Quality = fi.Quality
		if it.Quality == "" && fo != nil {
			it.Quality = fo.Quality
		}
		if it.Quality == "" {
			if w := probeWidth(f); w > 0 {
				it.Quality = QualityFromWidth(w)
				it.Notes = append(it.Notes, fmt.Sprintf("quality from video width %dpx", w))
			} else {
				it.Notes = append(it.Notes, "quality unknown: ask")
			}
		}
		if it.Year == 0 {
			if it.Type == "tv" {
				it.Notes = append(it.Notes, "year unknown")
			} else {
				it.Notes = append(it.Notes, "year unknown: look it up or ask")
			}
		}

		// Companions: same-stem files next to the video, plus loose subtitles in a single-video release folder.
		var comps []string
		stem := strings.ToLower(core.Stem(f))
		entries, _ := os.ReadDir(filepath.Dir(f))
		for _, e := range entries {
			c := filepath.Join(filepath.Dir(f), e.Name())
			if !e.IsDir() && c != f && core.CompanionExts[core.Ext(c)] && strings.HasPrefix(strings.ToLower(e.Name()), stem) {
				comps = append(comps, c)
			}
		}
		if lone {
			for _, c := range core.Walk(filepath.Join(root, top), core.SubtitleExts) {
				dup := false
				for _, x := range comps {
					dup = dup || x == c
				}
				if !dup {
					comps = append(comps, c)
				}
			}
		}
		used := map[string]bool{}
		for _, c := range comps {
			if claimed[c] {
				continue
			}
			claimed[c] = true
			ext := core.Ext(c)
			suffix := ext
			if core.SubtitleExts[ext] {
				suffix = LangSuffix(c, core.Stem(f)) + ext
			}
			for n, base := 2, suffix; used[suffix]; n++ {
				suffix = strings.TrimSuffix(base, ext) + fmt.Sprintf(".%d", n) + ext
			}
			used[suffix] = true
			it.Companions = append(it.Companions, Companion{core.Rel(root, c), suffix})
		}
		d.Items = append(d.Items, it)
	}

	if *titles {
		lookupTitles(d.Items)
	}
	for i := range d.Items {
		if d.Items[i].Type == "tv" && d.Items[i].EpisodeTitle == "" {
			d.Items[i].Notes = append(d.Items[i].Notes, "no episode title")
		}
	}

	if err := core.WriteJSON(pos[1], d); err != nil {
		return core.Fail("%v", err)
	}
	for _, it := range d.Items {
		target, err := LibraryPath(it)
		if err != nil {
			target = "?? (" + err.Error() + ")"
		}
		kind := "MOVIE"
		if it.Type == "tv" {
			kind = "TV   "
		}
		line := fmt.Sprintf("%s %s\n      -> %s%s", kind, it.Src, target, filepath.Ext(it.Src))
		if len(it.Companions) > 0 {
			line += fmt.Sprintf("  [+%d companion(s)]", len(it.Companions))
		}
		if len(it.Notes) > 0 {
			line += "\n      notes: " + strings.Join(it.Notes, "; ")
		}
		fmt.Println(line)
	}
	for _, s := range d.Skipped {
		fmt.Printf("SKIP  %s  (%s)\n", s.Src, s.Reason)
	}
	fmt.Printf("\n%d video(s) drafted -> %s  (movies -> %s, tv -> %s)\n", len(d.Items), pos[1], d.Dest["movies"], d.Dest["tv"])
	return 0
}

func lookupTitles(items []Item) {
	type key struct {
		title string
		year  int
	}
	type result struct {
		show  *Show
		cands []Show
	}
	shows := map[key]result{}
	for i := range items {
		it := &items[i]
		if it.Type != "tv" {
			continue
		}
		k := key{strings.ToLower(it.Title), it.Year}
		r, ok := shows[k]
		if !ok && it.Title != "" {
			s, c, err := BestShow(it.Title, it.Year)
			if err != nil {
				it.Notes = append(it.Notes, "TVmaze lookup failed: "+err.Error())
			}
			r = result{s, c}
			shows[k] = r
		}
		if r.show == nil {
			it.Notes = append(it.Notes, "no TVmaze match")
			continue
		}
		show := r.show
		it.TVmazeID = show.ID
		if show.Name != it.Title {
			it.Notes = append(it.Notes, fmt.Sprintf("series name from TVmaze (file: '%s')", it.Title))
			it.Title = show.Name
		}
		if y, _ := strconv.Atoi(show.Year); y > 0 && y != it.Year {
			if it.Year > 0 {
				it.Notes = append(it.Notes, fmt.Sprintf("TVmaze premiere year %d differs from file year %d", y, it.Year))
			} else {
				it.Year = y
				var keep []string
				for _, n := range it.Notes {
					if !strings.HasPrefix(n, "year unknown") {
						keep = append(keep, n)
					}
				}
				it.Notes = append([]string{}, keep...)
			}
		}
		for _, c := range r.cands {
			if strings.EqualFold(c.Name, show.Name) && c.ID != show.ID {
				it.Notes = append(it.Notes, fmt.Sprintf("several shows named '%s' (picked %s): check the year", show.Name, show.Year))
				break
			}
		}
		ep := EpisodeTitle(show.ID, *it.Season, *it.Episode)
		if ep == "" {
			it.Notes = append(it.Notes, "episode not found on TVmaze")
			continue
		}
		if it.EpisodeTitle != "" && !strings.EqualFold(it.EpisodeTitle, ep) {
			it.Notes = append(it.Notes, fmt.Sprintf("file had episode title '%s'", it.EpisodeTitle))
		}
		it.EpisodeTitle = ep
		if it.EpisodeEnd != nil {
			if ep2 := EpisodeTitle(show.ID, *it.Season, *it.EpisodeEnd); ep2 != "" && ep2 != ep {
				it.EpisodeTitle = ep + " + " + ep2
			}
		}
	}
}
