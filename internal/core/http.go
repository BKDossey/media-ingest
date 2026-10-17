package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotFound is returned for HTTP 404.
var ErrNotFound = errors.New("not found")

var client = &http.Client{Timeout: 40 * time.Second}

// UserAgent identifies media-ingest to the APIs it calls, plus the optional contact
// from the config (MusicBrainz asks for one).
func UserAgent() string {
	ua := "media-ingest/" + Version
	if cfg, err := LoadConfig(); err == nil && cfg.Contact != nil && *cfg.Contact != "" {
		ua += " ( " + *cfg.Contact + " )"
	}
	return ua
}

var ua string

// Get fetches a URL with retry and backoff on 429/5xx and network errors.
// Returns ErrNotFound on 404.
func Get(base string, params url.Values, retries int) ([]byte, error) {
	if ua == "" {
		ua = UserAgent()
	}
	u := base
	if len(params) > 0 {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + params.Encode()
	}
	var last error
	for attempt := 0; attempt < retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<attempt) * 1500 * time.Millisecond / 2)
		}
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode == 404:
			return nil, ErrNotFound
		case resp.StatusCode == 429 || resp.StatusCode >= 500:
			last = fmt.Errorf("HTTP %d from %s", resp.StatusCode, base)
			continue
		case resp.StatusCode >= 400:
			return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, base)
		case err != nil:
			last = err
			continue
		}
		return body, nil
	}
	return nil, last
}

// GetJSON fetches a URL and decodes JSON into v. A 404 leaves v untouched and returns ErrNotFound.
func GetJSON(base string, params url.Values, v any) error {
	body, err := Get(base, params, 4)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
