package video

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"media-ingest/internal/core"
)

const OrganizeDoc = `Usage: media-ingest organize-video <draft.json> [--dry-run]

Renames/moves movies and TV episodes into the library layout from a reviewed draft
(written by draft-video). Every name is built from the item fields, so editing "title",
"year", "quality", "episode_title" etc. is all it takes to fix a name. Items with
"skip": true are left alone.

Safety: never overwrites (an item whose destination exists is reported and not moved);
moves across drives/NAS mounts are copied, checksum-verified, then removed; never deletes
files. Source folders are removed only once completely empty, and folders that still
hold something (release-site .txt files, samples...) are listed.
`

type move struct{ src, dst string }

// RunOrganize implements "media-ingest organize-video".
func RunOrganize(args []string) int {
	fs := core.NewFlags("organize-video", OrganizeDoc)
	dry := fs.Bool("dry-run", false, "")
	pos, ok := core.Parse(fs, args)
	if !ok || len(pos) != 1 {
		fs.Usage()
		return 2
	}
	var d Draft
	if err := core.ReadJSON(pos[0], &d); err != nil {
		return core.Fail("%v", err)
	}
	dest := map[string]string{"movie": d.Dest["movies"], "tv": d.Dest["tv"]}

	// Resolve every move first, so clashes inside the batch are caught before anything changes.
	type job struct {
		it    Item
		moves []move
	}
	var jobs []job
	errors := 0
	targets := map[string]string{}
	for _, it := range d.Items {
		if it.Skip {
			fmt.Println("SKIPPED  " + it.Src)
			continue
		}
		lib, err := LibraryPath(it)
		if err != nil || dest[it.Type] == "" {
			if err == nil {
				err = fmt.Errorf("unknown type %q", it.Type)
			}
			fmt.Printf("NOT MOVED (%v): %s\n", err, it.Src)
			errors++
			continue
		}
		base := Join(dest[it.Type], lib)
		src := Join(d.Root, it.Src)
		moves := []move{{src, base + filepath.Ext(src)}}
		for _, c := range it.Companions {
			moves = append(moves, move{Join(d.Root, c.Src), base + c.Suffix})
		}
		var bad []string
		for _, m := range moves {
			if !core.Exists(m.src) {
				bad = append(bad, "missing source "+m.src)
			} else if core.Exists(m.dst) && !core.SameFile(m.src, m.dst) {
				bad = append(bad, "destination exists: "+m.dst)
			}
			k := strings.ToLower(m.dst)
			if prev, ok := targets[k]; ok && prev != m.src {
				bad = append(bad, "two items want "+m.dst)
			}
			targets[k] = m.src
		}
		if len(bad) > 0 {
			fmt.Printf("NOT MOVED: %s\n    %s\n", it.Src, strings.Join(bad, "\n    "))
			errors++
			continue
		}
		jobs = append(jobs, job{it, moves})
	}

	touched := map[string]bool{}
	for _, j := range jobs {
		for _, m := range j.moves {
			if m.src == m.dst {
				continue
			}
			if *dry {
				fmt.Printf("WOULD MOVE  %s\n         -> %s\n", m.src, m.dst)
				continue
			}
			if err := core.MoveFile(m.src, m.dst); err != nil {
				fmt.Printf("FAILED  %s -> %s: %v\n", m.src, m.dst, err)
				errors++
				continue
			}
			touched[filepath.Dir(m.src)] = true
			fmt.Printf("MOVED  %s\n    -> %s\n", core.Rel(d.Root, m.src), m.dst)
		}
	}

	if !*dry && len(touched) > 0 {
		var dirs []string
		for t := range touched {
			dirs = append(dirs, t)
		}
		left := core.RemoveEmptyDirs(dirs, d.Root)
		if len(left) > 0 {
			fmt.Println("\nFolders left behind because they still contain files (nothing was deleted):")
			var keys []string
			for k := range left {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				names := left[k]
				more := ""
				if len(names) > 8 {
					names, more = names[:8], " ..."
				}
				fmt.Printf("  %s: %s%s\n", k, strings.Join(names, ", "), more)
			}
		}
	}
	verb := "organized"
	if *dry {
		verb = "would be organized"
	}
	fmt.Printf("\n%d item(s) %s, %d problem(s)\n", len(jobs), verb, errors)
	if errors > 0 {
		return 1
	}
	return 0
}
