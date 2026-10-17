// Package video parses scene/release names and organizes movies and TV episodes.
//
// Library layout:
//
//	Movies: <Title> (<Year>) [<Qual>]/<Title> (<Year>) [<Qual>].<ext>
//	TV:     <Series> (<Year>) [<Qual>]/Season <SS>/S<SS>E<EE> - <Episode Title>.<ext>
package video

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"media-ingest/internal/core"
)

var Qualities = []string{"2160p", "1080p", "720p", "480p"}

var resPatterns = []struct {
	re *regexp.Regexp
	q  string
}{
	{regexp.MustCompile(`(?i)\b(2160p|4k|uhd)\b`), "2160p"},
	{regexp.MustCompile(`(?i)\b1080[pi]\b`), "1080p"},
	{regexp.MustCompile(`(?i)\b720p\b`), "720p"},
	{regexp.MustCompile(`(?i)\b(480p|576p|360p)\b`), "480p"},
}

// tech marks the start of release info: everything from the first match on is dropped.
var tech = regexp.MustCompile(`(?i)\b(2160p|1080[pi]|720p|480p|576p|4k|uhd|hdr10\+?|hdr|dolby\s?vision|sdr|blu-?ray|` +
	`bdrip|brrip|bdremux|remux|web-?dl|web-?rip|hdtv|hdrip|dvdrip|dvd|dvdscr|hdcam|x26[45]|h\s?26[45]|hevc|avc|xvid|` +
	`divx|10\s?bit|8\s?bit|aac\d?|ac3|e-?ac-?3|ddp?\d|dts(-hd)?|truehd|atmos|flac|mp3|opus|6ch|proper|repack|` +
	`extended|unrated|imax|remastered|amzn|dsnp|hmax|atvp|dual(\s?audio)?|subbed|yts(\.\w+)?|yify|rarbg|eztv|tgx|` +
	`psa|tigole|qxr|galaxyrg|internal|limited)\b`)

var (
	sxxexx     = regexp.MustCompile(`(?i)\bS(\d{1,2})\s?E(\d{1,3})(?:\s?-?\s?E?(\d{1,3}))?\b`)
	nxnn       = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{2,3})\b`)
	seasonOnly = regexp.MustCompile(`(?i)\b(?:S(\d{1,2})|Season\s?(\d{1,2}))\b`)
	digits     = regexp.MustCompile(`\d+`)
	sitePrefix = regexp.MustCompile(`(?i)^\s*(\[[^\]]*\]|www\.\S+\s*-?|\w+\.(com|org|net|to)\s*-)\s*`)
	bracketed  = regexp.MustCompile(`[\(\[][^\)\]]*[\)\]]`)
	countryTag = regexp.MustCompile(`^[\(\[][A-Z]{2,3}[\)\]]$`)
	emptyBrkt  = regexp.MustCompile(`[\[\(\{]\s*[\]\)\}]`)
	ws         = regexp.MustCompile(`\s+`)
	abbrevEnd  = regexp.MustCompile(`(?:\b[A-Za-z]\.){2,}$`)
	langSplit  = regexp.MustCompile(`[\s._\-]+`)
)

var langs = map[string]string{"english": "en", "eng": "en", "spanish": "es", "spa": "es", "french": "fr",
	"fre": "fr", "fra": "fr", "german": "de", "ger": "de", "deu": "de", "italian": "it", "ita": "it",
	"portuguese": "pt", "por": "pt", "dutch": "nl", "russian": "ru", "japanese": "ja", "jpn": "ja",
	"korean": "ko", "chinese": "zh", "arabic": "ar", "swedish": "sv", "norwegian": "no", "danish": "da",
	"finnish": "fi", "polish": "pl", "turkish": "tr", "greek": "el", "hebrew": "he", "hindi": "hi",
	"forced": "forced", "sdh": "sdh"}

var small = map[string]bool{"a": true, "an": true, "the": true, "and": true, "but": true, "or": true,
	"nor": true, "for": true, "so": true, "yet": true, "at": true, "by": true, "in": true, "of": true,
	"on": true, "to": true, "up": true, "as": true, "is": true, "vs": true, "via": true, "from": true,
	"with": true, "into": true}

// Info is what Parse extracts from one release name.
type Info struct {
	Kind         string // "movie", "tv", or "season"
	Title        string
	Year         int
	Quality      string
	Season       int
	Episode      int
	EpisodeEnd   int
	EpisodeTitle string
	HasEpisode   bool
}

// Despace turns dots/underscores into spaces, keeping abbreviations such as A.I. or S.W.A.T.
func Despace(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
	if strings.Count(name, " ") >= strings.Count(name, ".") { // already spaced: dots are part of the title
		return name
	}
	var out, run []string
	flush := func() {
		if len(run) >= 2 {
			out = append(out, strings.Join(run, ".")+".")
		} else {
			out = append(out, run...)
		}
		run = nil
	}
	for _, t := range strings.Split(name, ".") {
		if r := []rune(t); len(r) == 1 && unicode.IsLetter(r[0]) {
			run = append(run, t)
			continue
		}
		flush()
		if t != "" {
			out = append(out, t)
		}
	}
	flush()
	return strings.Join(out, " ")
}

func tidy(s string) string {
	s = emptyBrkt.ReplaceAllString(s, "")
	s = strings.Trim(ws.ReplaceAllString(s, " "), " -–—_,[({")
	if !abbrevEnd.MatchString(s) { // keep the final dot of "S.W.A.T."
		s = strings.TrimRight(s, ". ")
	}
	return s
}

// Titlecase fixes all-lower or ALL-UPPER titles; mixed case is left alone.
func Titlecase(s string) string {
	if s != strings.ToLower(s) && s != strings.ToUpper(s) {
		return s
	}
	allUpper := s == strings.ToUpper(s)
	words := strings.Split(s, " ")
	for i, w := range words {
		switch {
		case strings.Contains(strings.Trim(w, "."), "."):
			words[i] = strings.ToUpper(w) // abbreviations: A.I., S.W.A.T.
		case w == strings.ToUpper(w) && len(w) <= 3 && !allUpper:
		case i > 0 && small[strings.ToLower(w)]:
			words[i] = strings.ToLower(w)
		default:
			r := []rune(strings.ToLower(w))
			if len(r) > 0 {
				r[0] = unicode.ToUpper(r[0])
			}
			words[i] = string(r)
		}
	}
	return strings.Join(words, " ")
}

// QualityOf finds a resolution in a name.
func QualityOf(text string) string {
	for _, p := range resPatterns {
		if p.re.MatchString(text) {
			return p.q
		}
	}
	return ""
}

// QualityFromWidth maps a video's pixel width to a quality bucket.
func QualityFromWidth(w int) string {
	switch {
	case w <= 0:
		return ""
	case w >= 3200:
		return "2160p"
	case w >= 1700:
		return "1080p"
	case w >= 1200:
		return "720p"
	}
	return "480p"
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// years finds plausible release years (1920–2049) that are standalone 4-digit numbers.
func years(s string) [][2]int {
	var out [][2]int
	for _, m := range digits.FindAllStringIndex(s, -1) {
		if m[1]-m[0] == 4 {
			if y := atoi(s[m[0]:m[1]]); y >= 1920 && y <= 2049 {
				out = append(out, [2]int{m[0], m[1]})
			}
		}
	}
	return out
}

// Parse one release name (a folder name or a file stem).
func Parse(name string) Info {
	raw := name
	for sitePrefix.MatchString(raw) {
		raw = strings.TrimLeft(sitePrefix.ReplaceAllString(raw, ""), " -")
	}
	text := Despace(raw)
	info := Info{Kind: "movie", Quality: QualityOf(text)}

	var head string
	m := sxxexx.FindStringSubmatchIndex(text)
	isSxE := m != nil
	if m == nil {
		m = nxnn.FindStringSubmatchIndex(text)
	}
	if m != nil {
		info.Kind, info.HasEpisode = "tv", true
		info.Season, info.Episode = atoi(text[m[2]:m[3]]), atoi(text[m[4]:m[5]])
		if isSxE && m[6] >= 0 {
			if end := atoi(text[m[6]:m[7]]); end > info.Episode {
				info.EpisodeEnd = end
			}
		}
		head = text[:m[0]]
		tail := text[m[1]:]
		if t := tech.FindStringIndex(tail); t != nil {
			tail = tail[:t[0]]
		}
		if ep := tidy(tail); ep != "" && !(len(ep) == 4 && len(years(ep)) == 1) {
			info.EpisodeTitle = Titlecase(ep)
		}
	} else {
		s := seasonOnly.FindStringSubmatchIndex(text)
		t := tech.FindStringIndex(text)
		if s != nil && (t == nil || s[0] < t[0]) {
			info.Kind = "season"
			if s[2] >= 0 {
				info.Season = atoi(text[s[2]:s[3]])
			} else {
				info.Season = atoi(text[s[4]:s[5]])
			}
			head = text[:s[0]]
		} else if t != nil {
			head = text[:t[0]]
		} else {
			head = text
		}
	}

	// Year: prefer one in brackets, else the last plausible year that isn't the whole title.
	var cands, inBrackets [][2]int
	for _, y := range years(head) {
		if y[0] > 0 {
			cands = append(cands, y)
			if c := head[y[0]-1]; c == '(' || c == '[' {
				inBrackets = append(inBrackets, y)
			}
		}
	}
	if len(inBrackets) > 0 {
		cands = inBrackets
	}
	if len(cands) > 0 {
		y := cands[len(cands)-1]
		info.Year = atoi(head[y[0]:y[1]])
		head = head[:y[0]]
	}
	// Drop bracketed junk but keep country tags such as "(US)" / "(UK)".
	head = bracketed.ReplaceAllStringFunc(head, func(b string) string {
		if countryTag.MatchString(b) {
			return b
		}
		return ""
	})
	info.Title = Titlecase(tidy(head))
	return info
}

// LangSuffix is the ".en"-style suffix for a subtitle, from its name relative to the video.
func LangSuffix(subPath, videoStem string) string {
	stem := core.Stem(subPath)
	rest := stem
	if strings.HasPrefix(strings.ToLower(stem), strings.ToLower(videoStem)) {
		rest = stem[len(videoStem):]
	}
	var codes []string
	seen := map[string]bool{}
	for _, p := range langSplit.Split(strings.ToLower(rest), -1) {
		code := langs[p]
		if code == "" && len(p) == 2 && strings.HasPrefix(rest, ".") && isAlpha(p) {
			code = p
		}
		if code != "" && !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return ""
	}
	return "." + strings.Join(codes, ".")
}

func isAlpha(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// FmtEpisode renders S01E02 (or S01E02-E03).
func FmtEpisode(season, episode, end int) string {
	w := 2
	if season > 99 || episode > 99 || end > 99 {
		w = 3
	}
	s := fmt.Sprintf("S%0*dE%0*d", w, season, w, episode)
	if end > 0 {
		s += fmt.Sprintf("-E%0*d", w, end)
	}
	return s
}

// Item is one video in a draft; LibraryPath builds its name from these fields.
type Item struct {
	Src          string      `json:"src"`
	Type         string      `json:"type"`
	Title        string      `json:"title"`
	Year         int         `json:"year"`
	Quality      string      `json:"quality"`
	Season       *int        `json:"season"`
	Episode      *int        `json:"episode"`
	EpisodeEnd   *int        `json:"episode_end"`
	EpisodeTitle string      `json:"episode_title"`
	Companions   []Companion `json:"companions"`
	Notes        []string    `json:"notes"`
	TVmazeID     int         `json:"tvmaze_id,omitempty"`
	Skip         bool        `json:"skip,omitempty"`
}

type Companion struct {
	Src    string `json:"src"`
	Suffix string `json:"suffix"`
}

// LibraryPath is the item's path inside its library root, without extension, using "/".
func LibraryPath(it Item) (string, error) {
	var missing []string
	if it.Title == "" {
		missing = append(missing, "title")
	}
	if it.Year == 0 {
		missing = append(missing, "year")
	}
	if it.Quality == "" {
		missing = append(missing, "quality")
	}
	if it.Type == "tv" && (it.Season == nil || it.Episode == nil) {
		missing = append(missing, "season/episode")
	}
	if len(missing) > 0 {
		return "", errors.New("missing " + strings.Join(missing, ", "))
	}
	ok := false
	for _, q := range Qualities {
		ok = ok || q == it.Quality
	}
	if !ok {
		return "", errors.New("quality must be one of " + strings.Join(Qualities, ", "))
	}
	base := core.SafeName(fmt.Sprintf("%s (%d) [%s]", it.Title, it.Year, it.Quality))
	if it.Type == "movie" {
		return base + "/" + base, nil
	}
	end := 0
	if it.EpisodeEnd != nil {
		end = *it.EpisodeEnd
	}
	name := FmtEpisode(*it.Season, *it.Episode, end)
	if it.EpisodeTitle != "" {
		name += " - " + core.SafeName(it.EpisodeTitle)
	}
	w := 2
	if *it.Season > 99 {
		w = 3
	}
	return fmt.Sprintf("%s/Season %0*d/%s", base, w, *it.Season, name), nil
}

// Join builds an OS path from a root and a "/" library path.
func Join(root, rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
