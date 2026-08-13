package tui

import (
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// reqDelegate 自定义列表渲染：彩色方法 badge + 请求名，选中行整行高亮。
type reqDelegate struct{}

func (d reqDelegate) Height() int                             { return 1 }
func (d reqDelegate) Spacing() int                            { return 0 }
func (d reqDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d reqDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(listItem)
	if !ok || m.Width() <= 0 {
		return
	}

	const badgeW = 6
	bar := " "
	nameStyle := valueStyle
	if index == m.Index() {
		bar = lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render("▎")
	}

	nameW := m.Width() - 1 - badgeW - 1 // 左边条 + badge + 间隔
	name := ansi.Truncate(it.req.Name, nameW, "…")
	row := bar + methodBadge(it.req.Method) + " " + nameStyle.Render(name)

	if index == m.Index() {
		row = lipgloss.NewStyle().
			Background(colBgHighlight).
			Width(m.Width()).
			Render(row)
	}
	io.WriteString(w, row)
}
