package httputil

import (
	"io"
	"net/http"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// knownEncodings maps normalized (lowercase) definition `encoding:` values to
// their decoder. UTF-8/utf8 are intentionally absent: the decoder is nil and
// the body passes through unchanged. TIS-620 has no decoder in
// golang.org/x/text v0.40.0 (no thaitext package, ianaindex returns nil), so
// it is treated as unknown: passthrough + a Validate() warning.
var knownEncodings = map[string]encoding.Encoding{
	"windows-1251": charmap.Windows1251,
	"iso-8859-2":   charmap.ISO8859_2,
	"iso-8859-1":   charmap.ISO8859_1,
	"windows-1252": charmap.Windows1252,
	"windows-1250": charmap.Windows1250,
	"windows-874":  charmap.Windows874,
	"windows-1256": charmap.Windows1256,
	"windows-1255": charmap.Windows1255,
}

// NormalizeEncoding lowercases and trims an encoding name for lookup.
func NormalizeEncoding(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// KnownEncoding reports whether the encoding name is supported (UTF-8 family
// or present in the decoder table). Unknown names fall back to a UTF-8
// passthrough with a Validate() warning.
func KnownEncoding(name string) bool {
	switch NormalizeEncoding(name) {
	case "utf-8", "utf8":
		return true
	}
	_, ok := knownEncodings[NormalizeEncoding(name)]
	return ok
}

// EncodingDecoder returns the decoder for the encoding name, or nil when the
// name is unknown or UTF-8 (no decoding needed).
func EncodingDecoder(name string) encoding.Encoding {
	return knownEncodings[NormalizeEncoding(name)]
}

// ReadBodyLimited reads resp.Body, decoding it from the definition's declared
// `encoding:` when it is not UTF-8 (transform.NewReader wraps the body before
// the limit reader so decoded output is capped at limit bytes). Unknown
// encodings pass through unchanged.
func ReadBodyLimited(resp *http.Response, encodingName string, limit int64) ([]byte, error) {
	reader := io.Reader(resp.Body)
	if enc := EncodingDecoder(encodingName); enc != nil {
		reader = transform.NewReader(resp.Body, enc.NewDecoder())
	}
	return io.ReadAll(io.LimitReader(reader, limit))
}
