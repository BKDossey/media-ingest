// Command build cross-compiles media-ingest for every supported OS and CPU into
// skills/media-ingest/bin/, so the skill works without Go installed.
//
// Run it from the repository root before tagging a release:
//
//	go run ./tools/build
//
// Pure Go (CGO disabled), so every target builds from any machine.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var targets = [][2]string{
	{"windows", "amd64"}, {"windows", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"linux", "amd64"}, {"linux", "arm64"},
}

const outDir = "skills/media-ingest/bin"

func main() {
	if _, err := os.Stat("go.mod"); err != nil {
		fail("run this from the repository root (where go.mod is)")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fail(err.Error())
	}
	var built []string
	for _, t := range targets {
		name := fmt.Sprintf("media-ingest-%s-%s", t[0], t[1])
		if t[0] == "windows" {
			name += ".exe"
		}
		out := filepath.Join(outDir, name)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out, "./cmd/media-ingest")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t[0], "GOARCH="+t[1])
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fail("building " + name + ": " + err.Error())
		}
		st, _ := os.Stat(out)
		fmt.Printf("built %-40s %5.1f MB\n", out, float64(st.Size())/1e6)
		built = append(built, filepath.ToSlash(out))
	}
	// Git on Windows doesn't record the executable bit; set it in the index so macOS and
	// Linux checkouts can run the binaries directly.
	var unix []string
	for _, b := range built {
		if !strings.HasSuffix(b, ".exe") {
			unix = append(unix, b)
		}
	}
	if _, err := os.Stat(".git"); err == nil {
		add := exec.Command("git", append([]string{"add", "--"}, built...)...)
		chmod := exec.Command("git", append([]string{"update-index", "--chmod=+x", "--"}, unix...)...)
		if add.Run() == nil && chmod.Run() == nil {
			fmt.Println("staged in git, with the executable bit set on the macOS/Linux binaries")
		} else {
			fmt.Println("note: couldn't stage in git; run: git add " + outDir + " && git update-index --chmod=+x " + strings.Join(unix, " "))
		}
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "build:", msg)
	os.Exit(1)
}
