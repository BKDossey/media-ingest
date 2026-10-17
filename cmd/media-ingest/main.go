// Command media-ingest organizes movies, TV episodes, and music into a clean library.
// It is the engine behind the media-ingest Claude Code skill; every subcommand is also
// usable on its own. Run "media-ingest help" for the list.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"media-ingest/internal/core"
	"media-ingest/internal/music"
	"media-ingest/internal/video"
)

const usage = `media-ingest ` + core.Version + ` — organize movies, TV, and music into a clean library.

Usage: media-ingest <command> [arguments]

General
  scan <folder>                    sort a folder into video / audio / companions / other
  config show|path|init [--force]  show, locate, or create the config file
  version                          print the version

Movies and TV
  draft-video <folder> <out.json>  draft library names (--episode-titles for TVmaze lookups)
  organize-video <draft.json>      apply a reviewed video draft
  tv search <name>                 TVmaze show search
  tv episodes --show ID            TVmaze episode list (--season N)

Music (MP3 and FLAC)
  inspect <folder>                 dump tags, art, and lyrics sidecars
  draft-music <folder> <out.json>  draft canonical MusicBrainz tags for every song
  mb album|track|detail|cover      MusicBrainz lookups and cover download
  apply-music <plan.json>          write tags + cover, strip junk, move into the library
  apply-music --rename-only <dir>  move into the library using existing tags only
  lyrics <folder> | --files <list> fetch lyrics from LRCLIB
  release-tags <folder>            strip release-specific IDs (library repair)

Run "media-ingest <command> -h" for a command's details.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	commands := map[string]func([]string) int{
		"scan":           runScan,
		"config":         runConfig,
		"version":        func([]string) int { fmt.Println(core.Version); return 0 },
		"draft-video":    video.RunDraft,
		"organize-video": video.RunOrganize,
		"tv":             runTV,
		"inspect":        music.RunInspect,
		"draft-music":    music.RunDraft,
		"mb":             music.RunMB,
		"apply-music":    music.RunApply,
		"lyrics":         music.RunLyrics,
		"release-tags":   music.RunReleaseTags,
	}
	run, ok := commands[cmd]
	if !ok {
		if cmd != "help" && cmd != "-h" && cmd != "--help" {
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		}
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	os.Exit(run(args))
}

const scanDoc = `Usage: media-ingest scan <folder>

Read-only survey. Prints JSON: counts, video files, audio files grouped by folder,
companions (subtitles, .nfo, lyrics), and anything else. Hidden files are ignored.
`

func runScan(args []string) int {
	fs := core.NewFlags("scan", scanDoc)
	pos, ok := core.Parse(fs, args)
	if !ok || len(pos) != 1 {
		fs.Usage()
		return 2
	}
	root, _ := filepath.Abs(core.Expand(pos[0]))
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return core.Fail("not a folder: %s", root)
	}
	type vid struct {
		Path   string  `json:"path"`
		SizeMB float64 `json:"size_mb"`
	}
	videos, companions, other := []vid{}, []string{}, []string{}
	audio := map[string][]string{}
	nAudio := 0
	for _, f := range core.Walk(root, nil) {
		rel, ext := core.Rel(root, f), core.Ext(f)
		switch {
		case core.VideoExts[ext]:
			st, _ := os.Stat(f)
			mb, _ := strconv.ParseFloat(fmt.Sprintf("%.1f", float64(st.Size())/1e6), 64)
			videos = append(videos, vid{rel, mb})
		case core.AudioExts[ext]:
			dir := core.Rel(root, filepath.Dir(f))
			audio[dir] = append(audio[dir], filepath.Base(f))
			nAudio++
		case core.CompanionExts[ext] || core.LyricsExts[ext]:
			companions = append(companions, rel)
		default:
			other = append(other, rel)
		}
	}
	unsupported := []string{}
	for dir, files := range audio {
		for _, f := range files {
			if !core.TaggableAudio[core.Ext(f)] {
				unsupported = append(unsupported, filepath.ToSlash(filepath.Join(dir, f)))
			}
		}
	}
	sort.Strings(unsupported)
	core.PrintJSON(map[string]any{
		"root": root,
		"counts": map[string]int{"video": len(videos), "audio": nAudio, "audio_unsupported": len(unsupported),
			"companions": len(companions), "other": len(other)},
		"video": videos, "audio": audio, "audio_unsupported": unsupported,
		"companions": companions, "other": other,
	})
	return 0
}

const configDoc = `Usage: media-ingest config show|path|init [--force]

  show   print the effective config (defaults merged with the file) as JSON
  path   print where the config file lives
  init   write a starter config file with every option at its default

The config lives at ~/.config/media-ingest/config.json on every OS (or under
$XDG_CONFIG_HOME, or wherever $MEDIA_INGEST_CONFIG points). It is optional.
`

func runConfig(args []string) int {
	fs := core.NewFlags("config", configDoc)
	force := fs.Bool("force", false, "")
	pos, ok := core.Parse(fs, args)
	if !ok {
		return 2
	}
	sub := "show"
	if len(pos) > 0 {
		sub = pos[0]
	}
	switch sub {
	case "path":
		fmt.Println(core.ConfigPath())
	case "show":
		cfg, err := core.LoadConfig()
		if err != nil {
			return core.Fail("%v", err)
		}
		core.PrintJSON(cfg)
	case "init":
		p := core.ConfigPath()
		if core.Exists(p) && !*force {
			return core.Fail("%s already exists (use --force to overwrite)", p)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return core.Fail("%v", err)
		}
		if err := os.WriteFile(p, []byte(core.Starter), 0o644); err != nil {
			return core.Fail("%v", err)
		}
		fmt.Println("wrote " + p)
	default:
		fs.Usage()
		return 2
	}
	return 0
}

const tvDoc = `Usage:
  media-ingest tv search "<name>"                 candidate shows: id, name, premiere year, network
  media-ingest tv episodes --show <id> [--season N] episode list: season, episode, title, airdate

TVmaze lookups (free, no API key).
`

func runTV(args []string) int {
	fs := core.NewFlags("tv", tvDoc)
	show := fs.Int("show", 0, "")
	season := fs.Int("season", 0, "")
	pos, ok := core.Parse(fs, args)
	if !ok || len(pos) == 0 {
		fs.Usage()
		return 2
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	switch pos[0] {
	case "search":
		if len(pos) < 2 {
			fs.Usage()
			return 2
		}
		shows, err := video.SearchShows(pos[1])
		if err != nil {
			return core.Fail("%v", err)
		}
		for _, s := range shows {
			enc.Encode(s)
		}
	case "episodes":
		if *show == 0 {
			fs.Usage()
			return 2
		}
		eps, err := video.Episodes(*show, *season)
		if err != nil {
			return core.Fail("%v", err)
		}
		for _, e := range eps {
			enc.Encode(e)
		}
	default:
		fs.Usage()
		return 2
	}
	return 0
}
