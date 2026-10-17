package music

import (
	"bytes"
	"encoding/binary"
	"io"
	"unicode/utf16"
)

// ID3v2.3 frames written by media-ingest itself. bogem/id3v2 appends a stray zero byte
// after UTF-16 text, which corrupts any frame where text is followed by more data
// (TXXX values, picture data). These encode UTF-16 as the spec says: BOM (FF FE),
// little-endian code units, and a two-byte terminator between fields.

const encUTF16 = 1

func utf16le(s string) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xFE})
	for _, u := range utf16.Encode([]rune(s)) {
		binary.Write(&b, binary.LittleEndian, u)
	}
	return b.Bytes()
}

// rawFrame is an encoded frame body. uid tells repeated frames (TXXX, APIC...) apart.
type rawFrame struct {
	b   []byte
	uid string
}

func (f rawFrame) Size() int                { return len(f.b) }
func (f rawFrame) UniqueIdentifier() string { return f.uid }
func (f rawFrame) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(f.b)
	return int64(n), err
}

// textFrame is a T??? frame (TIT2, TPE1, ...).
func textFrame(text string) rawFrame {
	return rawFrame{append([]byte{encUTF16}, utf16le(text)...), ""}
}

// txxxFrame is a user-defined text frame.
func txxxFrame(desc, value string) rawFrame {
	b := append([]byte{encUTF16}, utf16le(desc)...)
	b = append(b, 0, 0)
	return rawFrame{append(b, utf16le(value)...), desc}
}

// apicFrame is an attached picture.
func apicFrame(mime string, picType byte, desc string, data []byte) rawFrame {
	b := []byte{encUTF16}
	b = append(b, mime...)
	b = append(b, 0, picType)
	b = append(b, utf16le(desc)...)
	b = append(b, 0, 0)
	return rawFrame{append(b, data...), string(picType) + desc}
}

// usltFrame is unsynchronised lyrics.
func usltFrame(lang, desc, text string) rawFrame {
	if len(lang) != 3 {
		lang = "eng"
	}
	b := append([]byte{encUTF16}, lang...)
	b = append(b, utf16le(desc)...)
	b = append(b, 0, 0)
	return rawFrame{append(b, utf16le(text)...), lang + desc}
}
