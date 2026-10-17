package music

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/go-flac/flacpicture"
	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"

	"media-ingest/internal/core"
)

// FlexString accepts a JSON string or number (so "date": 1994 works as well as "1994").
type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*f = FlexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = FlexString(n.String())
	return nil
}

// FlexInt accepts a JSON number or a numeric string (so "track": "2" works as well as 2).
type FlexInt int

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	var n int
	if json.Unmarshal(b, &n) == nil {
		*f = FlexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*f = FlexInt(firstInt(s))
	return nil
}

// NewTags is what a plan asks to write. Nil/empty fields are left as they are.
type NewTags struct {
	Title       *string     `json:"title,omitempty"`
	Artist      *string     `json:"artist,omitempty"`
	AlbumArtist *string     `json:"albumartist,omitempty"`
	Album       *string     `json:"album,omitempty"`
	Date        *FlexString `json:"date,omitempty"`
	Genre       *string     `json:"genre,omitempty"`
	Track       *FlexInt    `json:"track,omitempty"`
	TrackTotal  *FlexInt    `json:"tracktotal,omitempty"`
	Disc        *FlexInt    `json:"disc,omitempty"`
	DiscTotal   *FlexInt    `json:"disctotal,omitempty"`
}

func (t NewTags) Empty() bool {
	return t.Title == nil && t.Artist == nil && t.AlbumArtist == nil && t.Album == nil && t.Date == nil &&
		t.Genre == nil && t.Track == nil && t.TrackTotal == nil && t.Disc == nil && t.DiscTotal == nil
}

func pos(n *FlexInt, total *FlexInt) string {
	if total != nil && *total > 0 {
		return fmt.Sprintf("%d/%d", *n, *total)
	}
	return fmt.Sprint(*n)
}

// releaseKeys are release-specific tags. Files from different source releases (deluxe,
// compilation, standard CD) keep their old release IDs, track totals and dates, which
// makes music servers split one album into several. Recording/artist IDs stay.
var releaseKeys = map[string]bool{}

func init() {
	for _, k := range []string{
		"musicbrainz_albumid", "musicbrainz album id", "musicbrainz_releasetrackid", "musicbrainz release track id",
		"musicbrainz_albumtype", "musicbrainz album type", "musicbrainz_albumstatus", "musicbrainz album status",
		"releasecountry", "musicbrainz album release country", "releasestatus", "releasetype",
		"musicbrainz_albumcomment", "musicbrainz album comment", "musicbrainz_releasegroupid",
		"musicbrainz release group id", "barcode", "catalognumber", "asin", "media", "totaltracks",
		"totaldiscs", "trackc", "discc", "originaldate", "originalyear", "releasedate", "albumversion",
		"year", "script", "replaygain_album_gain", "replaygain_album_peak",
	} {
		releaseKeys[k] = true
	}
}

var (
	id3ReleaseFrames = []string{"TDOR", "TMED", "TDRL", "TORY"}
	id3Junk          = []string{"COMM", "PRIV", "MCDI", "TLEN", "GEOB", "NCON", "USER", "WXXX", "TENC", "TSSE"}
	id3Lyrics        = []string{"USLT", "SYLT"}
	id3V24Only       = []string{"TDEN", "TDTG"} // dropped when saving as ID3v2.3
	txxxKeep         = []string{"MusicBrainz", "REPLAYGAIN", "replaygain", "ASIN", "BARCODE", "ARTISTS",
		"Acoustid", "CATALOGNUMBER", "originalyear", "SCRIPT"}
	flacJunk   = []string{"COMMENT", "DESCRIPTION", "ENCODED-BY", "ENCODER", "ITUNNORM", "ITUNSMPB"}
	flacLyrics = []string{"LYRICS", "UNSYNCEDLYRICS"}
)

// WriteOptions controls what WriteTags changes besides the given tags.
type WriteOptions struct {
	Cover        []byte // replaces every embedded picture with one front cover
	ReleaseGroup string // MusicBrainz release group chosen for the album
	StripLyrics  bool   // also remove embedded lyrics
}

// WriteTags writes tags to an MP3 (as ID3v2.3) or FLAC, stripping release-specific tags and junk.
func WriteTags(path string, t NewTags, o WriteOptions) error {
	switch core.Ext(path) {
	case ".mp3":
		return writeMP3(path, t, o)
	case ".flac":
		return writeFLAC(path, t, o)
	}
	return ErrUnsupported
}

// ---------------------------------------------------------------- MP3

func stripReleaseID3(tag *id3v2.Tag) []string {
	var removed []string
	for _, id := range id3ReleaseFrames {
		if len(tag.GetFrames(id)) > 0 {
			tag.DeleteFrames(id)
			removed = append(removed, id)
		}
	}
	var keep []id3v2.UserDefinedTextFrame
	for _, f := range tag.GetFrames("TXXX") {
		u, ok := f.(id3v2.UserDefinedTextFrame)
		if !ok {
			continue
		}
		if releaseKeys[strings.ToLower(u.Description)] {
			removed = append(removed, u.Description)
		} else {
			keep = append(keep, u)
		}
	}
	if len(removed) > 0 {
		tag.DeleteFrames("TXXX")
		for _, u := range keep {
			tag.AddUserDefinedTextFrame(u)
		}
	}
	return removed
}

// reencodeTXXX rewrites every TXXX frame with correct UTF-16 (see id3frames.go).
func reencodeTXXX(tag *id3v2.Tag) {
	frames := tag.GetFrames("TXXX")
	tag.DeleteFrames("TXXX")
	for _, f := range frames {
		if u, ok := f.(id3v2.UserDefinedTextFrame); ok {
			tag.AddFrame("TXXX", txxxFrame(u.Description, u.Value))
		} else {
			tag.AddFrame("TXXX", f)
		}
	}
}

func writeMP3(path string, t NewTags, o WriteOptions) error {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer tag.Close()

	// Always save as ID3v2.3 (UTF-16 text): the most widely readable version.
	year := strings.TrimSpace(tag.GetTextFrame("TDRC").Text)
	if year == "" {
		year = strings.TrimSpace(tag.GetTextFrame("TYER").Text)
	}
	tag.DeleteFrames("TDRC")
	for _, id := range id3V24Only {
		tag.DeleteFrames(id)
	}
	tag.SetVersion(3)

	stripReleaseID3(tag)
	for _, id := range id3Junk {
		tag.DeleteFrames(id)
	}
	if o.StripLyrics {
		for _, id := range id3Lyrics {
			tag.DeleteFrames(id)
		}
	}

	// Rebuild every frame media-ingest understands with correct UTF-16 (see id3frames.go).
	for id, frames := range tag.AllFrames() {
		switch {
		case id == "TXXX":
			tag.DeleteFrames(id)
			for _, f := range frames {
				u, ok := f.(id3v2.UserDefinedTextFrame)
				if !ok {
					continue
				}
				for _, p := range txxxKeep {
					if strings.HasPrefix(u.Description, p) {
						tag.AddFrame("TXXX", txxxFrame(u.Description, u.Value))
						break
					}
				}
			}
		case strings.HasPrefix(id, "T"):
			if tf, ok := frames[0].(id3v2.TextFrame); ok {
				tag.DeleteFrames(id)
				tag.AddFrame(id, textFrame(tf.Text))
			}
		case id == "APIC":
			tag.DeleteFrames(id)
			for _, f := range frames {
				if pf, ok := f.(id3v2.PictureFrame); ok {
					tag.AddFrame(id, apicFrame(pf.MimeType, pf.PictureType, pf.Description, pf.Picture))
				}
			}
		case id == "USLT":
			tag.DeleteFrames(id)
			for _, f := range frames {
				if lf, ok := f.(id3v2.UnsynchronisedLyricsFrame); ok {
					tag.AddFrame(id, usltFrame(lf.Language, lf.ContentDescriptor, lf.Lyrics))
				}
			}
		}
	}
	if o.ReleaseGroup != "" {
		tag.AddFrame("TXXX", txxxFrame("MusicBrainz Release Group Id", o.ReleaseGroup))
	}

	set := func(id string, v *string) {
		if v != nil {
			tag.AddFrame(id, textFrame(*v))
		}
	}
	set("TIT2", t.Title)
	set("TPE1", t.Artist)
	set("TPE2", t.AlbumArtist)
	set("TALB", t.Album)
	set("TCON", t.Genre)
	if t.Date != nil {
		year = string(*t.Date)
	}
	if len(year) >= 4 {
		tag.AddFrame("TYER", textFrame(year[:4]))
	}
	if t.Track != nil {
		tag.AddFrame("TRCK", textFrame(pos(t.Track, t.TrackTotal)))
	}
	if t.Disc != nil {
		tag.AddFrame("TPOS", textFrame(pos(t.Disc, t.DiscTotal)))
	}
	if o.Cover != nil {
		tag.DeleteFrames("APIC")
		tag.AddFrame("APIC", apicFrame(mimeOf(o.Cover), id3v2.PTFrontCover, "Cover", o.Cover))
	}
	if err := tag.Save(); err != nil {
		return err
	}
	tag.Close()
	return stripID3v1(path)
}

// ---------------------------------------------------------------- FLAC

type vorbis struct {
	*flacvorbis.MetaDataBlockVorbisComment
}

func (v vorbis) del(keys ...string) []string {
	var keep, removed []string
	for _, c := range v.Comments {
		k := strings.SplitN(c, "=", 2)[0]
		hit := false
		for _, x := range keys {
			hit = hit || strings.EqualFold(k, x)
		}
		if hit {
			removed = append(removed, k)
		} else {
			keep = append(keep, c)
		}
	}
	v.Comments = keep
	return removed
}

func (v vorbis) set(key, val string) {
	v.del(key)
	v.Comments = append(v.Comments, key+"="+val)
}

func stripReleaseVorbis(v vorbis) []string {
	var keys []string
	for _, c := range v.Comments {
		k := strings.SplitN(c, "=", 2)[0]
		if releaseKeys[strings.ToLower(k)] {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return v.del(keys...)
}

func loadFLAC(path string) (file *flac.File, v vorbis, idx int, err error) {
	defer func() { // go-flac panics on some malformed files; report them as errors instead
		if r := recover(); r != nil {
			file, idx, err = nil, -1, fmt.Errorf("unreadable FLAC: %v", r)
		}
	}()
	file, err = flac.ParseFile(path)
	if err != nil {
		return nil, vorbis{}, -1, err
	}
	for i, b := range file.Meta {
		if b.Type == flac.VorbisComment {
			c, err := flacvorbis.ParseFromMetaDataBlock(*b)
			if err != nil {
				return nil, vorbis{}, -1, err
			}
			return file, vorbis{c}, i, nil
		}
	}
	return file, vorbis{flacvorbis.New()}, -1, nil
}

func putVorbis(file *flac.File, v vorbis, idx int) {
	block := v.Marshal()
	if idx >= 0 {
		file.Meta[idx] = &block
	} else {
		file.Meta = append(file.Meta, &block)
	}
}

func saveFLAC(path string, file *flac.File, v vorbis, idx int) error {
	putVorbis(file, v, idx)
	return writeFLACFile(path, file)
}

func writeFLACFile(path string, file *flac.File) error {
	tmp := path + ".mi-tmp"
	if err := os.WriteFile(tmp, file.Marshal(), 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func writeFLAC(path string, t NewTags, o WriteOptions) error {
	file, v, idx, err := loadFLAC(path)
	if err != nil {
		return err
	}
	stripReleaseVorbis(v)
	if o.ReleaseGroup != "" {
		v.set("MUSICBRAINZ_RELEASEGROUPID", o.ReleaseGroup)
	}
	v.del(flacJunk...)
	if o.StripLyrics {
		v.del(flacLyrics...)
	}
	var nonEmpty []string
	for _, c := range v.Comments {
		if kv := strings.SplitN(c, "=", 2); len(kv) == 2 && strings.TrimSpace(kv[1]) != "" {
			nonEmpty = append(nonEmpty, c)
		}
	}
	v.Comments = nonEmpty
	str := func(key string, val *string) {
		if val != nil {
			v.set(key, *val)
		}
	}
	num := func(key string, val *FlexInt) {
		if val != nil {
			v.set(key, fmt.Sprint(*val))
		}
	}
	str("TITLE", t.Title)
	str("ARTIST", t.Artist)
	str("ALBUMARTIST", t.AlbumArtist)
	str("ALBUM", t.Album)
	if t.Date != nil {
		v.set("DATE", string(*t.Date))
	}
	str("GENRE", t.Genre)
	num("TRACKNUMBER", t.Track)
	num("TRACKTOTAL", t.TrackTotal)
	num("DISCNUMBER", t.Disc)
	num("DISCTOTAL", t.DiscTotal)

	putVorbis(file, v, idx)
	if o.Cover != nil {
		var meta []*flac.MetaDataBlock
		for _, b := range file.Meta {
			if b.Type != flac.Picture {
				meta = append(meta, b)
			}
		}
		file.Meta = meta
		pic, err := flacpicture.NewFromImageData(flacpicture.PictureTypeFrontCover, "Cover", o.Cover, mimeOf(o.Cover))
		if err != nil { // undecodable image: embed it anyway, without dimensions
			pic = &flacpicture.MetadataBlockPicture{PictureType: flacpicture.PictureTypeFrontCover,
				MIME: mimeOf(o.Cover), Description: "Cover", ImageData: o.Cover}
		}
		pb := pic.Marshal()
		file.Meta = append(file.Meta, &pb)
	}
	return writeFLACFile(path, file)
}

// StripRelease removes release-specific tags from one file (for release-tags).
// It returns what was (or, with dry, would be) removed.
func StripRelease(path string, dry bool) ([]string, error) {
	switch core.Ext(path) {
	case ".mp3":
		tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
		if err != nil {
			return nil, err
		}
		defer tag.Close()
		removed := stripReleaseID3(tag)
		if len(removed) > 0 && !dry {
			reencodeTXXX(tag)
			if err := tag.Save(); err != nil {
				return nil, err
			}
		}
		return removed, nil
	case ".flac":
		file, v, idx, err := loadFLAC(path)
		if err != nil {
			return nil, err
		}
		removed := stripReleaseVorbis(v)
		if len(removed) > 0 && !dry {
			if err := saveFLAC(path, file, v, idx); err != nil {
				return nil, err
			}
		}
		return removed, nil
	}
	return nil, nil
}
