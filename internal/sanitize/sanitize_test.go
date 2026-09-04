package sanitize

import (
	"strings"
	"testing"

	"github.com/HsiuCheYu/quietdm.nvim/internal/model"
)

func TestTextStripsEmoji(t *testing.T) {
	opts := Options{StripEmoji: true}
	cases := []struct {
		in   string
		want string
	}{
		{"晚上要吃什麼 😂", "晚上要吃什麼 :D"},
		{"👍", "+1"},
		{"好喔 ❤️", "好喔 <3"},
		{"家人 👨‍👩‍👧‍👦 出遊", "家人 出遊"},
		{"讚 👍🏽", "讚 +1"},
		{"純文字", "純文字"},
	}
	for _, tc := range cases {
		if got := Text(tc.in, opts); got != tc.want {
			t.Errorf("Text(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTextKeepsEmojiWhenDisabled(t *testing.T) {
	if got := Text("hi 😂", Options{}); got != "hi 😂" {
		t.Errorf("got %q, want the emoji left alone", got)
	}
}

func TestTextCollapsesNewlines(t *testing.T) {
	// A raw newline would break NDJSON framing and cannot be shown as virtual
	// text either.
	got := Text("first\nsecond\r\n\tthird", Options{})
	if strings.ContainsAny(got, "\n\r\t") {
		t.Fatalf("got %q, want no control characters", got)
	}
	if got != "first second third" {
		t.Errorf("got %q", got)
	}
}

func TestTextCapsLength(t *testing.T) {
	long := strings.Repeat("晚", 100) // 300 bytes
	got := Text(long, Options{MaxBody: 31})
	if len(got) > 31 {
		t.Fatalf("len = %d, want <= 31", len(got))
	}
	if strings.ContainsRune(got, '�') {
		t.Error("cut a rune in half")
	}
}

func TestMessagePlaceholders(t *testing.T) {
	for kind, want := range map[string]string{
		model.KindImage:   "[圖片]",
		model.KindAudio:   "[語音]",
		model.KindSticker: "[貼圖]",
		model.KindOther:   "[附件]",
	} {
		m := Message(model.Message{Kind: kind, Body: "https://example.org/cat.png"}, Options{})
		if m.Body != want {
			t.Errorf("kind %s: got %q, want %q", kind, m.Body, want)
		}
	}
}

func TestMessageTextIsSanitised(t *testing.T) {
	m := Message(model.Message{Kind: model.KindText, Body: "yo 😂"}, Options{StripEmoji: true})
	if m.Body != "yo :D" {
		t.Errorf("got %q", m.Body)
	}
}
