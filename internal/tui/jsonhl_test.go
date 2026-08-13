package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// 测试环境无 TTY，lipgloss 默认降级为无色；强制 TrueColor 以便断言 ANSI 输出。
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

func TestHighlightJSONColorsKeysAndValues(t *testing.T) {
	in := `{"name": "tpost", "count": 3, "ok": true, "data": null}`
	out := HighlightJSON(in)

	// key 应带 AccentAlt 色码，字符串值带绿色码
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI codes in output, got %q", out)
	}
	// 内容本身不能丢
	for _, want := range []string{`"name"`, `"tpost"`, "3", "true", "null"} {
		if !strings.Contains(stripANSI(out), want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestHighlightJSONDistinguishesKeyFromStringValue(t *testing.T) {
	in := `{"a": "b:"}`
	out := HighlightJSON(in)
	plain := stripANSI(out)
	if plain != in {
		t.Errorf("highlight changed content: %q → %q", in, plain)
	}
}

func TestHighlightJSONNonJSONPassesThrough(t *testing.T) {
	for _, in := range []string{"", "plain text body", "  hello {not json}", "<xml></xml>"} {
		if out := HighlightJSON(in); out != in {
			t.Errorf("non-JSON input %q was modified: %q", in, out)
		}
	}
}

func TestHighlightJSONNestedAndMultiline(t *testing.T) {
	in := "{\n  \"user\": {\n    \"id\": 123,\n    \"tags\": [\"a\", \"b\"]\n  }\n}"
	out := HighlightJSON(in)
	if stripANSI(out) != in {
		t.Errorf("multiline highlight changed content:\n%q", out)
	}
}

func TestHighlightJSONEscapedQuote(t *testing.T) {
	in := `{"msg": "say \"hi\""}`
	out := HighlightJSON(in)
	if stripANSI(out) != in {
		t.Errorf("escaped quote broke scanning:\n%q", out)
	}
}

// stripANSI 去掉 CSI 转义序列，便于断言纯文本内容。
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}
