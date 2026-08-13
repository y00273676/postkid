package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	jsonKeyStyle    = lipgloss.NewStyle().Foreground(colAccentAlt)
	jsonStringStyle = lipgloss.NewStyle().Foreground(colGreen)
	jsonNumberStyle = lipgloss.NewStyle().Foreground(colOrange)
	jsonBoolStyle   = lipgloss.NewStyle().Foreground(colAccentAlt).Bold(true)
	jsonNullStyle   = lipgloss.NewStyle().Foreground(colRed)
	jsonPunctStyle  = lipgloss.NewStyle().Foreground(colMuted)
)

// HighlightJSON 给 JSON 文本上语法色。非 JSON 输入原样返回。
// 手写轻量状态机，只处理 key/string/number/bool/null/标点。
func HighlightJSON(s string) string {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) * 2)
	rs := []rune(s)
	i := 0
	for i < len(rs) {
		c := rs[i]
		switch {
		case c == '"':
			end := scanString(rs, i)
			str := string(rs[i:end])
			if isKeyPosition(rs, end) {
				b.WriteString(jsonKeyStyle.Render(str))
			} else {
				b.WriteString(jsonStringStyle.Render(str))
			}
			i = end
		case c == '-' || (c >= '0' && c <= '9'):
			end := i
			for end < len(rs) && isNumberRune(rs[end]) {
				end++
			}
			b.WriteString(jsonNumberStyle.Render(string(rs[i:end])))
			i = end
		case c == 't' && matchLiteral(rs, i, "true"):
			b.WriteString(jsonBoolStyle.Render("true"))
			i += 4
		case c == 'f' && matchLiteral(rs, i, "false"):
			b.WriteString(jsonBoolStyle.Render("false"))
			i += 5
		case c == 'n' && matchLiteral(rs, i, "null"):
			b.WriteString(jsonNullStyle.Render("null"))
			i += 4
		case c == '{' || c == '}' || c == '[' || c == ']' || c == ',' || c == ':':
			b.WriteString(jsonPunctStyle.Render(string(c)))
			i++
		default:
			b.WriteRune(c)
			i++
		}
	}
	return b.String()
}

// scanString 返回从 rs[start]（开引号）开始的字符串结束位置（闭引号+1）。
func scanString(rs []rune, start int) int {
	for i := start + 1; i < len(rs); i++ {
		if rs[i] == '\\' {
			i++
			continue
		}
		if rs[i] == '"' {
			return i + 1
		}
	}
	return len(rs)
}

// isKeyPosition 判断字符串结束位置 end 之后（跳过空白）是否是 ':'。
func isKeyPosition(rs []rune, end int) bool {
	for i := end; i < len(rs); i++ {
		switch rs[i] {
		case ' ', '\t':
			continue
		case ':':
			return true
		default:
			return false
		}
	}
	return false
}

func matchLiteral(rs []rune, start int, lit string) bool {
	lr := []rune(lit)
	if start+len(lr) > len(rs) {
		return false
	}
	for j, c := range lr {
		if rs[start+j] != c {
			return false
		}
	}
	return true
}

func isNumberRune(c rune) bool {
	return c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' || (c >= '0' && c <= '9')
}
