package video

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"media-ingest/internal/core"
)

const tvmaze = "https://api.tvmaze.com"

// Show is a TVmaze search result.
type Show struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Year    string  `json:"year"`
	Network string  `json:"network"`
	Score   float64 `json:"score"`
}

// Episode is one TVmaze episode.
type Episode struct {
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Title   string `json:"title"`
	Airdate string `json:"airdate"`
}

type mazeShow struct {
	ID         int                    `json:"id"`
	Name       string                 `json:"name"`
	Premiered  string                 `json:"premiered"`
	Network    *struct{ Name string } `json:"network"`
	WebChannel *struct{ Name string } `json:"webChannel"`
}

// SearchShows lists TVmaze candidates for a series name.
func SearchShows(name string) ([]Show, error) {
	var res []struct {
		Score float64  `json:"score"`
		Show  mazeShow `json:"show"`
	}
	if err := core.GetJSON(tvmaze+"/search/shows", url.Values{"q": {name}}, &res); err != nil && err != core.ErrNotFound {
		return nil, err
	}
	out := make([]Show, 0, len(res))
	for _, r := range res {
		s := Show{ID: r.Show.ID, Name: r.Show.Name, Score: float64(int(r.Score*1000)) / 1000}
		if len(r.Show.Premiered) >= 4 {
			s.Year = r.Show.Premiered[:4]
		}
		if r.Show.Network != nil {
			s.Network = r.Show.Network.Name
		} else if r.Show.WebChannel != nil {
			s.Network = r.Show.WebChannel.Name
		}
		out = append(out, s)
	}
	return out, nil
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]`)

func normName(s string) string {
	return nonAlnum.ReplaceAllString(strings.ReplaceAll(strings.ToLower(s), "&", "and"), "")
}

// BestShow picks the TVmaze match for a parsed series name (and year, when known).
func BestShow(name string, year int) (*Show, []Show, error) {
	cands, err := SearchShows(name)
	if err != nil || len(cands) == 0 {
		return nil, cands, err
	}
	pool := cands
	var exact []Show
	for _, c := range cands {
		if normName(c.Name) == normName(name) {
			exact = append(exact, c)
		}
	}
	if len(exact) > 0 {
		pool = exact
	}
	if year > 0 {
		var dated []Show
		for _, c := range pool {
			if c.Year == fmt.Sprint(year) {
				dated = append(dated, c)
			}
		}
		if len(dated) > 0 {
			pool = dated
		}
	}
	best := pool[0]
	return &best, cands, nil
}

var epCache = map[int][]Episode{}

// Episodes lists a show's episodes (season 0 = all seasons).
func Episodes(showID, season int) ([]Episode, error) {
	if _, ok := epCache[showID]; !ok {
		var res []struct {
			Season  int    `json:"season"`
			Number  int    `json:"number"`
			Name    string `json:"name"`
			Airdate string `json:"airdate"`
		}
		if err := core.GetJSON(fmt.Sprintf("%s/shows/%d/episodes", tvmaze, showID), url.Values{"specials": {"1"}}, &res); err != nil && err != core.ErrNotFound {
			return nil, err
		}
		eps := make([]Episode, 0, len(res))
		for _, r := range res {
			eps = append(eps, Episode{r.Season, r.Number, r.Name, r.Airdate})
		}
		epCache[showID] = eps
	}
	var out []Episode
	for _, e := range epCache[showID] {
		if season == 0 || e.Season == season {
			out = append(out, e)
		}
	}
	return out, nil
}

// EpisodeTitle is the title of one episode, or "" when TVmaze doesn't list it.
func EpisodeTitle(showID, season, episode int) string {
	eps, _ := Episodes(showID, season)
	for _, e := range eps {
		if e.Episode == episode {
			return e.Title
		}
	}
	return ""
}
