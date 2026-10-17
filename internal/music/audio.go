// Package music reads and writes MP3/FLAC tags, looks up MusicBrainz and LRCLIB, and
// organizes songs into <Album Artist>/<Album> (<Year>)/<NN> - <Title>.<ext>.
package music

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/go-flac/flacpicture"
	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"

	"media-ingest/internal/core"
)

// Song is what media-ingest needs to know about one audio file.
type Song struct {
	Path        string
	Format      string // "mp3" or "flac"
	Duration    float64
	BitrateKbps int
	SampleRate  int
	Channels    int
	BitDepth    int // FLAC only
	TagVersion  string
	Tags        Tags              // normalized fields
	Raw         map[string]string // every tag, described for inspect
	Pictures    []string          // described for inspect
}

// Tags are the normalized fields media-ingest names files by.
type Tags struct {
	Title, Artist, AlbumArtist, Album, Date, Genre string
	Track, TrackTotal, Disc, DiscTotal             int
	ReleaseGroup                                   string
}

// ErrUnsupported is returned for audio formats other than MP3 and FLAC.
var ErrUnsupported = errors.New("unsupported format (only MP3 and FLAC are handled)")

var leadInt = regexp.MustCompile(`^\s*(\d+)`)

func firstInt(s string) int {
	if m := leadInt.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// splitPos parses "3/12" into (3, 12).
func splitPos(s string) (int, int) {
	parts := strings.SplitN(s, "/", 2)
	n := firstInt(parts[0])
	if len(parts) == 2 {
		return n, firstInt(parts[1])
	}
	return n, 0
}

// Read loads tags and stream info for an MP3 or FLAC file.
func Read(path string) (*Song, error) {
	switch core.Ext(path) {
	case ".mp3":
		return readMP3(path)
	case ".flac":
		return readFLAC(path)
	}
	return nil, ErrUnsupported
}

// ---------------------------------------------------------------- MP3

func readMP3(path string) (*Song, error) {
	s := &Song{Path: path, Format: "mp3", Raw: map[string]string{}}
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return nil, err
	}
	defer tag.Close()
	if tag.HasFrames() {
		s.TagVersion = fmt.Sprintf("2.%d", tag.Version())
	}
	text := func(id string) string { return strings.TrimSpace(tag.GetTextFrame(id).Text) }
	s.Tags.Title, s.Tags.Artist, s.Tags.AlbumArtist = text("TIT2"), text("TPE1"), text("TPE2")
	s.Tags.Album, s.Tags.Genre = text("TALB"), text("TCON")
	s.Tags.Date = text("TDRC")
	if s.Tags.Date == "" {
		s.Tags.Date = text("TYER")
	}
	s.Tags.Track, s.Tags.TrackTotal = splitPos(text("TRCK"))
	s.Tags.Disc, s.Tags.DiscTotal = splitPos(text("TPOS"))
	for id, frames := range tag.AllFrames() {
		for _, f := range frames {
			key, val := id, ""
			switch fr := f.(type) {
			case id3v2.TextFrame:
				val = fr.Text
			case id3v2.UserDefinedTextFrame:
				key, val = "TXXX:"+fr.Description, fr.Value
				if strings.EqualFold(fr.Description, "MusicBrainz Release Group Id") {
					s.Tags.ReleaseGroup = fr.Value
				}
			case id3v2.CommentFrame:
				key, val = "COMM:"+fr.Description, fr.Text
			case id3v2.UnsynchronisedLyricsFrame:
				key, val = "USLT", fmt.Sprintf("<lyrics, %d chars>", len(fr.Lyrics))
			case id3v2.PictureFrame:
				key = "APIC:" + fr.Description
				val = fmt.Sprintf("<picture type=%d %s %dB>", fr.PictureType, fr.MimeType, len(fr.Picture))
				s.Pictures = append(s.Pictures, fmt.Sprintf("type=%d %s %dB", fr.PictureType, fr.MimeType, len(fr.Picture)))
			default:
				val = "<binary>"
			}
			if len(val) > 300 {
				val = val[:300]
			}
			s.Raw[key] = val
		}
	}
	if err := mp3Stream(path, s); err != nil {
		return s, nil // tags are still useful without stream info
	}
	return s, nil
}

var (
	bitratesV1L1 = []int{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448}
	bitratesV1L2 = []int{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384}
	bitratesV1L3 = []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	bitratesV2L1 = []int{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256}
	bitratesV2L3 = []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	sampleRates  = map[int][]int{3: {44100, 48000, 32000}, 2: {22050, 24000, 16000}, 0: {11025, 12000, 8000}}
)

type mpegHeader struct {
	version, layer, bitrate, sampleRate, channels, samples int
	mono                                                   bool
}

func parseMPEG(h []byte) (mpegHeader, bool) {
	if len(h) < 4 || h[0] != 0xFF || h[1]&0xE0 != 0xE0 {
		return mpegHeader{}, false
	}
	v := int(h[1]>>3) & 3 // 3 = MPEG1, 2 = MPEG2, 0 = MPEG2.5
	l := int(h[1]>>1) & 3 // 3 = Layer I, 2 = Layer II, 1 = Layer III
	bi := int(h[2] >> 4)
	si := int(h[2]>>2) & 3
	if v == 1 || l == 0 || bi == 0 || bi == 15 || si == 3 {
		return mpegHeader{}, false
	}
	var table []int
	switch {
	case v == 3 && l == 3:
		table = bitratesV1L1
	case v == 3 && l == 2:
		table = bitratesV1L2
	case v == 3:
		table = bitratesV1L3
	case l == 3:
		table = bitratesV2L1
	default:
		table = bitratesV2L3
	}
	m := mpegHeader{version: v, layer: l, bitrate: table[bi] * 1000, sampleRate: sampleRates[v][si]}
	m.mono = (h[3] >> 6) == 3
	m.channels = 2
	if m.mono {
		m.channels = 1
	}
	switch {
	case l == 3:
		m.samples = 384
	case l == 1 && v != 3:
		m.samples = 576
	default:
		m.samples = 1152
	}
	return m, true
}

// mp3Stream fills duration, bitrate, sample rate, and channels from the first MPEG frame,
// using a Xing/Info or VBRI header when present, else assuming constant bitrate.
func mp3Stream(path string, s *Song) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, _ := f.Stat()
	size := st.Size()

	start := int64(0)
	head := make([]byte, 10)
	if _, err := io.ReadFull(f, head); err == nil && string(head[:3]) == "ID3" {
		tagSize := int64(head[6]&0x7f)<<21 | int64(head[7]&0x7f)<<14 | int64(head[8]&0x7f)<<7 | int64(head[9]&0x7f)
		start = 10 + tagSize
		if head[5]&0x10 != 0 {
			start += 10 // footer
		}
	}
	buf := make([]byte, 128*1024)
	n, _ := f.ReadAt(buf, start)
	buf = buf[:n]
	for i := 0; i+4 <= len(buf); i++ {
		m, ok := parseMPEG(buf[i:])
		if !ok {
			continue
		}
		s.SampleRate, s.Channels = m.sampleRate, m.channels
		frame := buf[i:]
		// Xing/Info header offset depends on version and channel mode.
		off := 36
		switch {
		case m.version == 3 && m.mono:
			off = 21
		case m.version != 3 && !m.mono:
			off = 21
		case m.version != 3:
			off = 13
		}
		end := size - (start + int64(i))
		if hasID3v1(f, size) {
			end -= 128
		}
		if len(frame) > off+12 && (string(frame[off:off+4]) == "Xing" || string(frame[off:off+4]) == "Info") {
			flags := binary.BigEndian.Uint32(frame[off+4:])
			if flags&1 != 0 {
				frames := binary.BigEndian.Uint32(frame[off+8:])
				s.Duration = float64(frames) * float64(m.samples) / float64(m.sampleRate)
			}
		} else if len(frame) > 36+18 && string(frame[36:40]) == "VBRI" {
			frames := binary.BigEndian.Uint32(frame[36+14:])
			s.Duration = float64(frames) * float64(m.samples) / float64(m.sampleRate)
		}
		if s.Duration > 0 {
			s.BitrateKbps = int(float64(end) * 8 / s.Duration / 1000)
		} else {
			s.BitrateKbps = m.bitrate / 1000
			s.Duration = float64(end) * 8 / float64(m.bitrate)
		}
		return nil
	}
	return errors.New("no MPEG frame found")
}

func hasID3v1(f *os.File, size int64) bool {
	if size < 128 {
		return false
	}
	b := make([]byte, 3)
	f.ReadAt(b, size-128)
	return string(b) == "TAG"
}

// stripID3v1 removes a trailing ID3v1 tag, which media-ingest never keeps.
func stripID3v1(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	st, _ := f.Stat()
	if hasID3v1(f, st.Size()) {
		return f.Truncate(st.Size() - 128)
	}
	return nil
}

// ---------------------------------------------------------------- FLAC

func readFLAC(path string) (*Song, error) {
	s := &Song{Path: path, Format: "flac", Raw: map[string]string{}}
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	file, err := flac.ParseMetadata(fh)
	if err != nil {
		return nil, err
	}
	if info, err := file.GetStreamInfo(); err == nil {
		s.SampleRate, s.Channels, s.BitDepth = info.SampleRate, info.ChannelCount, info.BitDepth
		if info.SampleRate > 0 {
			s.Duration = float64(info.SampleCount) / float64(info.SampleRate)
		}
		if st, err := fh.Stat(); err == nil && s.Duration > 0 {
			s.BitrateKbps = int(float64(st.Size()) * 8 / s.Duration / 1000)
		}
	}
	for _, b := range file.Meta {
		switch b.Type {
		case flac.VorbisComment:
			cmts, err := flacvorbis.ParseFromMetaDataBlock(*b)
			if err != nil {
				continue
			}
			for _, c := range cmts.Comments {
				kv := strings.SplitN(c, "=", 2)
				if len(kv) != 2 {
					continue
				}
				k := strings.ToUpper(kv[0])
				if old, ok := s.Raw[k]; ok {
					kv[1] = old + "; " + kv[1]
				}
				if len(kv[1]) > 300 {
					kv[1] = kv[1][:300]
				}
				s.Raw[k] = kv[1]
			}
			get := func(keys ...string) string {
				for _, k := range keys {
					if v, _ := cmts.Get(k); len(v) > 0 && strings.TrimSpace(v[0]) != "" {
						return strings.TrimSpace(v[0])
					}
				}
				return ""
			}
			t := &s.Tags
			t.Title, t.Artist, t.Album = get("TITLE"), get("ARTIST"), get("ALBUM")
			t.AlbumArtist = get("ALBUMARTIST", "ALBUM ARTIST", "ALBUM_ARTIST")
			t.Date, t.Genre = get("DATE", "YEAR"), get("GENRE")
			t.Track, t.TrackTotal = splitPos(get("TRACKNUMBER"))
			if t.TrackTotal == 0 {
				t.TrackTotal = firstInt(get("TRACKTOTAL", "TOTALTRACKS"))
			}
			t.Disc, t.DiscTotal = splitPos(get("DISCNUMBER"))
			if t.DiscTotal == 0 {
				t.DiscTotal = firstInt(get("DISCTOTAL", "TOTALDISCS"))
			}
			t.ReleaseGroup = get("MUSICBRAINZ_RELEASEGROUPID")
		case flac.Picture:
			if p, err := flacpicture.ParseFromMetaDataBlock(*b); err == nil {
				s.Pictures = append(s.Pictures, fmt.Sprintf("type=%d %s %dx%d %dB", p.PictureType, p.MIME, p.Width, p.Height, len(p.ImageData)))
			}
		}
	}
	return s, nil
}

func mimeOf(data []byte) string {
	if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return "image/png"
	}
	return "image/jpeg"
}
