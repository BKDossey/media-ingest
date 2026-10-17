package music

import (
	"fmt"
	"math"
	"path/filepath"

	"media-ingest/internal/core"
)

const InspectDoc = `Usage: media-ingest inspect <folder>

Read-only. Prints JSON for every audio file: format, duration, bitrate, every tag,
embedded pictures, and lyrics sidecars. Formats other than MP3/FLAC are listed as
unsupported. Hidden files and folders are ignored.
`

// RunInspect implements "media-ingest inspect".
func RunInspect(args []string) int {
	fs := core.NewFlags("inspect", InspectDoc)
	pos, ok := core.Parse(fs, args)
	if !ok || len(pos) != 1 {
		fs.Usage()
		return 2
	}
	root, _ := filepath.Abs(core.Expand(pos[0]))
	out := []map[string]any{}
	for _, f := range core.Walk(root, core.AudioExts) {
		e := map[string]any{"path": core.Rel(root, f)}
		s, err := Read(f)
		if err != nil {
			e["audio"] = "UNREADABLE (" + err.Error() + ")"
			out = append(out, e)
			continue
		}
		audio := fmt.Sprintf("%s, %d Hz, %d ch, %.2f s", s.Format, s.SampleRate, s.Channels, s.Duration)
		if s.BitDepth > 0 {
			audio += fmt.Sprintf(", %d-bit", s.BitDepth)
		}
		var sidecars []string
		for _, sc := range []string{".lrc", ".txt"} {
			if core.Exists(core.WithExt(f, sc)) {
				sidecars = append(sidecars, sc)
			}
		}
		e["audio"] = audio
		e["duration"] = math.Round(s.Duration*100) / 100
		e["bitrate_kbps"] = s.BitrateKbps
		e["tag_version"] = s.TagVersion
		e["tags"] = s.Raw
		e["pictures"] = nonNil(s.Pictures)
		e["sidecars"] = nonNil(sidecars)
		out = append(out, e)
	}
	core.PrintJSON(out)
	return 0
}

const ReleaseTagsDoc = `Usage: media-ingest release-tags <folder> [--dry-run]

Strips release-specific tags (MusicBrainz release/album IDs, release-track IDs, alternate
track totals, original/release dates, barcode, catalog number, album ReplayGain) from
every MP3/FLAC under <folder>. apply-music does this to every song it writes; run this on
an existing library where one album shows up as several in your music server.
`

// RunReleaseTags implements "media-ingest release-tags".
func RunReleaseTags(args []string) int {
	fs := core.NewFlags("release-tags", ReleaseTagsDoc)
	dry := fs.Bool("dry-run", false, "")
	pos, ok := core.Parse(fs, args)
	if !ok || len(pos) != 1 {
		fs.Usage()
		return 2
	}
	root, _ := filepath.Abs(core.Expand(pos[0]))
	changed, failed := 0, 0
	for _, f := range core.Walk(root, core.TaggableAudio) {
		removed, err := StripRelease(f, *dry)
		if err != nil {
			fmt.Printf("FAILED %s: %v\n", core.Rel(root, f), err)
			failed++
			continue
		}
		if len(removed) > 0 {
			changed++
			verb := "STRIPPED"
			if *dry {
				verb = "WOULD STRIP"
			}
			fmt.Printf("%s %s: %v\n", verb, core.Rel(root, f), removed)
		}
	}
	verb := "changed"
	if *dry {
		verb = "would change"
	}
	fmt.Printf("\n%d file(s) %s, %d failed\n", changed, verb, failed)
	if failed > 0 {
		return 1
	}
	return 0
}
