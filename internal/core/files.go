package core

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

var (
	VideoExts     = set(".mkv", ".mp4", ".m4v", ".avi", ".mov", ".wmv", ".ts", ".m2ts", ".webm", ".mpg", ".mpeg", ".flv")
	AudioExts     = set(".mp3", ".flac", ".m4a", ".ogg", ".opus", ".wma", ".aac", ".wav", ".aiff", ".alac", ".ape", ".wv")
	TaggableAudio = set(".mp3", ".flac") // formats media-ingest reads and writes tags for
	SubtitleExts  = set(".srt", ".ass", ".ssa", ".sub", ".idx", ".vtt", ".sup")
	CompanionExts = set(".srt", ".ass", ".ssa", ".sub", ".idx", ".vtt", ".sup", ".nfo")
	LyricsExts    = set(".lrc", ".txt")
)

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// Ext is the lower-cased extension of a path.
func Ext(p string) string { return strings.ToLower(filepath.Ext(p)) }

// Stem is the file name without its extension.
func Stem(p string) string { b := filepath.Base(p); return strings.TrimSuffix(b, filepath.Ext(b)) }

// WithExt swaps a path's extension.
func WithExt(p, ext string) string { return strings.TrimSuffix(p, filepath.Ext(p)) + ext }

// Hidden reports whether any part of a relative path starts with ".".
func Hidden(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return true
		}
	}
	return false
}

// Walk lists files under root (recursively, sorted case-insensitively), skipping hidden
// files and folders. exts filters by extension; nil means every file.
func Walk(root string, exts map[string]bool) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && (exts == nil || exts[Ext(p)]) {
			out = append(out, p)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// Rel is filepath.Rel with forward slashes, falling back to the full path.
func Rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

var (
	illegal  = regexp.MustCompile(`[<>"|?*\x00-\x1f]`)
	spaces   = regexp.MustCompile(`\s+`)
	reserved = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\..*)?$`)
)

// SafeName makes a string safe as a file or folder name on every OS (Windows is the strictest).
func SafeName(name string) string {
	r := strings.NewReplacer(":", " -", "/", "-", `\`, "-", "’", "'", "‘", "'")
	name = illegal.ReplaceAllString(r.Replace(name), "")
	name = strings.TrimRight(strings.TrimSpace(spaces.ReplaceAllString(name, " ")), ". ")
	if reserved.MatchString(name) {
		name = "_" + name
	}
	return name
}

// MD5 of a file's contents.
func MD5(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SameFile reports whether two paths are the same file on disk (e.g. differ only in case).
func SameFile(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

// Exists reports whether a path exists.
func Exists(p string) bool { _, err := os.Stat(p); return err == nil }

// ErrExists is returned by MoveFile when the destination is already taken.
var ErrExists = errors.New("destination already exists")

// MoveFile moves src to dst without ever overwriting.
//   - Same filesystem: atomic rename.
//   - Case-only rename on a case-insensitive filesystem: goes through a temporary name.
//   - Different filesystem (another drive, a NAS mount): copy to "<dst>.partial", compare
//     MD5 checksums, rename into place, and only then delete the source.
func MoveFile(src, dst string) error {
	if src == dst {
		return nil
	}
	if Exists(dst) {
		if !SameFile(src, dst) {
			return fmt.Errorf("%w: %s", ErrExists, dst)
		}
		tmp := src + ".mi-tmp"
		if err := os.Rename(src, tmp); err != nil {
			return err
		}
		return os.Rename(tmp, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	err := os.Rename(src, dst)
	if err == nil || !crossDevice(err) {
		return err
	}
	part := dst + ".partial"
	if err := copyFile(src, part); err != nil {
		os.Remove(part)
		return err
	}
	a, err1 := MD5(src)
	b, err2 := MD5(part)
	if err1 != nil || err2 != nil || a != b {
		os.Remove(part)
		return fmt.Errorf("checksum mismatch copying %s -> %s; source kept", src, dst)
	}
	if err := os.Rename(part, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func crossDevice(err error) bool {
	var le *os.LinkError
	if !errors.As(err, &le) {
		return false
	}
	var errno syscall.Errno
	if errors.As(le.Err, &errno) {
		// EXDEV on Unix; ERROR_NOT_SAME_DEVICE (17) on Windows.
		return errno == syscall.EXDEV || uintptr(errno) == 17
	}
	return false
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, st.ModTime(), st.ModTime())
}

// RemoveEmptyDirs removes each folder, then its parents, while completely empty, never
// going above stopAt. Only empty folders are removed, so nothing can be lost. Returns the
// folders left behind because they still hold something, with their contents.
func RemoveEmptyDirs(dirs []string, stopAt string) map[string][]string {
	stop, _ := filepath.Abs(stopAt)
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	left := map[string][]string{}
	for _, d := range dirs {
		cur, _ := filepath.Abs(d)
		for cur != stop && strings.HasPrefix(cur, stop+string(filepath.Separator)) && Exists(cur) {
			if err := os.Remove(cur); err != nil {
				entries, _ := os.ReadDir(cur)
				var names []string
				for _, e := range entries {
					names = append(names, e.Name())
				}
				left[cur] = names
				break
			}
			delete(left, cur)
			cur = filepath.Dir(cur)
		}
	}
	for d := range left {
		if !Exists(d) {
			delete(left, d)
		}
	}
	return left
}

// PrintJSON writes v as indented JSON to stdout.
func PrintJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	enc.Encode(v)
}

// WriteJSON writes v as indented JSON to a file.
func WriteJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(v); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadJSON decodes a JSON file into v.
func ReadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
