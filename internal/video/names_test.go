package video

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name, kind, title string
		year              int
		quality           string
		season, episode   int
		epTitle           string
	}{
		{"Peter Pan (2003) (1080p BluRay x265 HEVC 10bit AAC 5.1 Tigole)", "movie", "Peter Pan", 2003, "1080p", 0, 0, ""},
		{"Hachi.A.Dog's.Tale.2009.BluRay.1080p.x264.AAC.5.1.-.Hon3y", "movie", "Hachi A Dog's Tale", 2009, "1080p", 0, 0, ""},
		{"The.Impossible.2012.1080p.BluRay.H264.AAC-RBG", "movie", "The Impossible", 2012, "1080p", 0, 0, ""},
		{"The.In.Between.2022.1080p.10bit.WEBRip.6CH.x265.HEVC-PSA", "movie", "The In Between", 2022, "1080p", 0, 0, ""},
		{"A.I. Artificial Intelligence (2001) (1080p BluRay x265 HEVC 10bit AAC 6.1 Silence)", "movie", "A.I. Artificial Intelligence", 2001, "1080p", 0, 0, ""},
		{"Uptown Girls (2003) [1080p] [YTS.AG]", "movie", "Uptown Girls", 2003, "1080p", 0, 0, ""},
		{"Charlotte's.Web.2006.720p.BluRay", "movie", "Charlotte's Web", 2006, "720p", 0, 0, ""},
		{"Blade.Runner.2049.2017.2160p.UHD", "movie", "Blade Runner 2049", 2017, "2160p", 0, 0, ""},
		{"2001.A.Space.Odyssey.1968.1080p", "movie", "2001 A Space Odyssey", 1968, "1080p", 0, 0, ""},
		{"1917.2019.1080p.WEB-DL", "movie", "1917", 2019, "1080p", 0, 0, ""},
		{"[www.example.com] - Dune.Part.Two.2024.2160p", "movie", "Dune Part Two", 2024, "2160p", 0, 0, ""},
		{"S.W.A.T.2017.S03E04.1080p.WEB.h264-GROUP", "tv", "S.W.A.T.", 2017, "1080p", 3, 4, ""},
		{"Silo.S02E01.The.Engineer.1080p.ATVP.WEB-DL", "tv", "Silo", 0, "1080p", 2, 1, "The Engineer"},
		{"The Office (US) S02E01-E02 720p", "tv", "The Office (US)", 0, "720p", 2, 1, ""},
		{"the.walking.dead.s01e01.720p", "tv", "The Walking Dead", 0, "720p", 1, 1, ""},
		{"show_name_1x05_hdtv", "tv", "Show Name", 0, "", 1, 5, ""},
		{"Breaking.Bad.S05.1080p.BluRay.x265", "season", "Breaking Bad", 0, "1080p", 5, 0, ""},
	}
	for _, c := range cases {
		got := Parse(c.name)
		if got.Kind != c.kind || got.Title != c.title || got.Year != c.year || got.Quality != c.quality ||
			got.Season != c.season || got.Episode != c.episode || got.EpisodeTitle != c.epTitle {
			t.Errorf("Parse(%q)\n got  %+v\n want kind=%s title=%q year=%d q=%s S%dE%d ep=%q",
				c.name, got, c.kind, c.title, c.year, c.quality, c.season, c.episode, c.epTitle)
		}
	}
}

func TestLibraryPath(t *testing.T) {
	s, e := 2, 1
	got, err := LibraryPath(Item{Type: "tv", Title: "Silo", Year: 2023, Quality: "1080p", Season: &s, Episode: &e, EpisodeTitle: "The Engineer"})
	if err != nil || got != "Silo (2023) [1080p]/Season 02/S02E01 - The Engineer" {
		t.Errorf("tv: %q %v", got, err)
	}
	got, _ = LibraryPath(Item{Type: "movie", Title: "He's Just Not: That Into You", Year: 2009, Quality: "1080p"})
	if got != "He's Just Not - That Into You (2009) [1080p]/He's Just Not - That Into You (2009) [1080p]" {
		t.Errorf("movie: %q", got)
	}
	if _, err := LibraryPath(Item{Type: "movie", Title: "X", Year: 2000}); err == nil {
		t.Error("missing quality should fail")
	}
}

func TestLangSuffix(t *testing.T) {
	for _, c := range [][3]string{
		{"Movie.en.srt", "Movie", ".en"},
		{"Subs/2_English.srt", "Movie", ".en"},
		{"Movie.English.Forced.srt", "Movie", ".en.forced"},
		{"Movie.srt", "Movie", ""},
	} {
		if got := LangSuffix(c[0], c[1]); got != c[2] {
			t.Errorf("LangSuffix(%q) = %q, want %q", c[0], got, c[2])
		}
	}
}
