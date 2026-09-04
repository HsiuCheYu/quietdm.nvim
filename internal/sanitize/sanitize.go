// Package sanitize turns raw chat text into something that can sit inside a
// code buffer without catching the eye.
//
// The rules come from docs/design/01-covert-model.md section 4: emoji are the
// single biggest give-away on a monochrome screen, so they are either mapped to
// an ASCII kaomoji or dropped entirely.
package sanitize

import (
	"strings"
	"unicode"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

// Options controls the sanitiser. The zero value strips emoji and applies no
// size cap.
type Options struct {
	// StripEmoji removes or folds emoji into ASCII.
	StripEmoji bool
	// MaxBody caps the body length in bytes as a protocol safety net (a single
	// NDJSON line may not exceed 64 KiB). It is NOT presentation truncation:
	// deciding how much fits on screen belongs to the renderer.
	MaxBody int
}

// DefaultMaxBody is the safety cap applied when Options.MaxBody is zero.
const DefaultMaxBody = 8192

// commonEmoji maps the emoji people actually send to their ASCII ancestors.
// Anything not listed here is dropped by stripEmoji.
var commonEmoji = map[rune]string{
	'\U0001F600': ":)",  // grinning
	'\U0001F601': ":D",  // beaming
	'\U0001F602': ":D",  // tears of joy
	'\U0001F603': ":)",  // smiling open mouth
	'\U0001F604': ":)",  // smiling
	'\U0001F605': ":')", // sweat smile
	'\U0001F606': ":D",  // laughing
	'\U0001F609': ";)",  // wink
	'\U0001F60A': ":)",  // blushing
	'\U0001F60D': "<3",  // heart eyes
	'\U0001F614': ":(",  // pensive
	'\U0001F622': ":'(", // crying
	'\U0001F62D': ":'(", // loudly crying
	'\U0001F621': ">:(", // pouting
	'\U0001F643': ":)",  // upside down
	'\U0001F644': ":|",  // eye roll
	'\U0001F60E': "B)",  // sunglasses
	'\U0001F914': ":?",  // thinking
	'\U0001F44D': "+1",  // thumbs up
	'\U0001F44E': "-1",  // thumbs down
	'\U0001F64F': "thx", // folded hands
	'\U0001F525': "!",   // fire
	'❤':          "<3",  // red heart
	'♥':          "<3",  // black heart suit
	'⭐':          "*",   // star
	'✅':          "ok",  // check mark button
	'❌':          "x",   // cross mark
}

// Text applies the sanitiser to a single body of text.
func Text(s string, opts Options) string {
	if opts.StripEmoji {
		s = stripEmoji(s)
	}
	s = collapse(s)
	max := opts.MaxBody
	if max == 0 {
		max = DefaultMaxBody
	}
	if max > 0 && len(s) > max {
		s = truncateBytes(s, max)
	}
	return s
}

// Message sanitises a message in place and returns it. Non-text kinds get a
// placeholder body: images and stickers cannot be disguised as code.
func Message(m model.Message, opts Options) model.Message {
	if p, ok := placeholder(m.Kind); ok {
		m.Body = p
		return m
	}
	m.Body = Text(m.Body, opts)
	return m
}

func placeholder(kind string) (string, bool) {
	switch kind {
	case model.KindImage:
		return "[圖片]", true
	case model.KindAudio:
		return "[語音]", true
	case model.KindSticker:
		return "[貼圖]", true
	case model.KindOther:
		return "[附件]", true
	}
	return "", false
}

// stripEmoji folds known emoji to ASCII and removes every other pictograph,
// including the joiners and modifiers that hold sequences together.
func stripEmoji(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if repl, ok := commonEmoji[r]; ok {
			b.WriteString(repl)
			continue
		}
		if isEmoji(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isEmoji reports whether r belongs to a range that renders as a coloured
// pictograph, or is one of the invisible characters used to build emoji
// sequences (ZWJ, variation selectors, skin tone modifiers, keycaps).
func isEmoji(r rune) bool {
	switch {
	case r == 0x200D, // zero width joiner
		r == 0xFE0E, r == 0xFE0F, // variation selectors
		r == 0x20E3: // combining enclosing keycap
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // skin tone modifiers
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF: // regional indicators (flags)
		return true
	case r >= 0x2190 && r <= 0x21FF: // arrows
		return true
	case r >= 0x2300 && r <= 0x23FF: // misc technical (⌚ ⏰ …)
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols + dingbats
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // misc symbols and arrows
		return true
	case r >= 0x1F000 && r <= 0x1FAFF: // the emoji planes
		return true
	case r >= 0x1FB00 && r <= 0x1FBFF: // legacy computing symbols
		return true
	}
	return false
}

// collapse folds newlines and control characters into single spaces. A raw
// newline would break the NDJSON framing, and a message that spans lines cannot
// be rendered as virtual text anyway.
func collapse(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || (unicode.IsControl(r) && r != ' ') {
			space = true
			continue
		}
		if r == ' ' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteRune(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// truncateBytes cuts s to at most max bytes without splitting a rune.
func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
