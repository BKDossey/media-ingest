// Package core holds what every media-ingest command shares: config, file discovery,
// safe moves, HTTP, and output helpers. Standard library only.
package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Version is the media-ingest release, sent in the User-Agent and printed by "version".
const Version = "1.0.0"

// Choice is a config option that can be true, false, or "ask".
type Choice struct{ V any }

func (c Choice) Ask() bool                    { s, ok := c.V.(string); return ok && strings.EqualFold(s, "ask") }
func (c Choice) On() bool                     { b, ok := c.V.(bool); return ok && b }
func (c Choice) MarshalJSON() ([]byte, error) { return json.Marshal(c.V) }
func (c *Choice) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &c.V)
}

type Destinations struct {
	Movies *string `json:"movies"`
	TV     *string `json:"tv"`
	Music  *string `json:"music"`
}

type VideoOptions struct {
	EpisodeTitles Choice `json:"episode_titles"`
}

type MusicOptions struct {
	Metadata    Choice `json:"metadata"`
	Lyrics      Choice `json:"lyrics"`
	PlainLyrics bool   `json:"plain_lyrics"`
}

type Config struct {
	Source       *string      `json:"source"`
	Destinations Destinations `json:"destinations"`
	Video        VideoOptions `json:"video"`
	Music        MusicOptions `json:"music"`
	Contact      *string      `json:"contact"`
	AfterIngest  []string     `json:"after_ingest"`

	Path   string `json:"_path"`
	Exists bool   `json:"_exists"`
}

// DefaultConfig is what every option is when the config file doesn't set it.
func DefaultConfig() Config {
	return Config{
		Video:       VideoOptions{EpisodeTitles: Choice{"ask"}},
		Music:       MusicOptions{Metadata: Choice{"ask"}, Lyrics: Choice{"ask"}},
		AfterIngest: []string{},
	}
}

// ConfigPath is $MEDIA_INGEST_CONFIG, else $XDG_CONFIG_HOME/media-ingest/config.json,
// else ~/.config/media-ingest/config.json, on every OS.
func ConfigPath() string {
	if p := os.Getenv("MEDIA_INGEST_CONFIG"); p != "" {
		return Expand(p)
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "media-ingest", "config.json")
}

// LoadConfig reads the config file over the defaults. A missing file is not an error.
func LoadConfig() (Config, error) {
	cfg := DefaultConfig()
	cfg.Path = ConfigPath()
	b, err := os.ReadFile(cfg.Path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, errors.New(cfg.Path + ": " + err.Error())
	}
	cfg.Exists = true
	if cfg.AfterIngest == nil {
		cfg.AfterIngest = []string{}
	}
	return cfg, nil
}

// Starter is the file "config init" writes: every option at its default.
const Starter = `{
  "source": null,
  "destinations": { "movies": null, "tv": null, "music": null },
  "video": { "episode_titles": "ask" },
  "music": { "metadata": "ask", "lyrics": "ask", "plain_lyrics": false },
  "contact": null,
  "after_ingest": []
}
`

// Expand resolves ~ and environment variables in a path.
func Expand(p string) string {
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[1:])
	}
	return p
}

// Pick returns the first non-empty of: an explicit flag value, a config value, a fallback.
func Pick(flag string, cfg *string, fallback string) string {
	if flag != "" {
		return Expand(flag)
	}
	if cfg != nil && *cfg != "" {
		return Expand(*cfg)
	}
	return fallback
}
