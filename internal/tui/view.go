package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

var (
	accent  = lipgloss.Color("63")
	muted   = lipgloss.Color("240")
	danger  = lipgloss.Color("203")
	success = lipgloss.Color("42")

	promptStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	tabActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(accent).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Foreground(muted).Padding(0, 1)
	dirtyMark   = lipgloss.NewStyle().Foreground(danger).Bold(true)
	errStyle    = lipgloss.NewStyle().Foreground(danger)
	okStyle     = lipgloss.NewStyle().Foreground(success)
)

func borderStyle(focused bool) lipgloss.Style {
	fg := muted
	if focused {
		fg = accent
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(fg).Padding(0, 1)
}

// View 渲染整个界面。
func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	if m.showHelp {
		return m.helpView()
	}

	listW := max(m.width*28/100, 24)
	rightW := m.width - listW
	contentH := m.height - 1 // 底部状态栏占 1 行
	reqH := max(contentH*45/100, 6)
	respH := max(contentH-reqH, 3)

	left := borderStyle(m.focus == FocusList).
		Width(listW).Height(contentH).
		Render(m.list.View())

	reqContent := m.renderRequest(rightW-2, reqH-2)
	reqBox := borderStyle(m.focus == FocusRequest).
		Width(rightW).Height(reqH).
		Render(reqContent)

	respContent := m.renderResponse()
	respBox := borderStyle(m.focus == FocusResponse).
		Width(rightW).Height(respH).
		Render(respContent)

	right := lipgloss.JoinVertical(lipgloss.Left, reqBox, respBox)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.renderBottom())
}

// renderRequest 渲染右上请求面板：方法+URL、tab bar、当前 tab 内容。
func (m Model) renderRequest(width, height int) string {
	if m.curReq == nil {
		return titleStyle.Render("No request selected")
	}

	method := m.curReq.Method
	url := m.curReq.URL
	if m.app != nil {
		// 展示已解析的 URL（变量替换后），便于确认实际请求地址
		if r, err := m.app.ResolveRequest(*m.curReq, *m.curColl); err == nil {
			url = r.URL
		}
	}
	top := fmt.Sprintf("%s  %s", okStyle.Render(method), truncate(url, width-len(method)-2))

	tabs := m.renderTabs()
	content := m.renderTabContent(height - 2)

	return lipgloss.JoinVertical(lipgloss.Left, top, tabs, content)
}

func (m Model) renderTabs() string {
	names := []string{"Params", "Headers", "Body"}
	var parts []string
	for i, n := range names {
		if tab(i) == m.tab {
			parts = append(parts, tabActive.Render(n))
		} else {
			parts = append(parts, tabInactive.Render(n))
		}
	}
	return strings.Join(parts, " ")
}

func (m Model) renderTabContent(height int) string {
	var lines []string
	switch m.tab {
	case TabParams:
		lines = renderKV(m.curReq.Params, height)
	case TabHeaders:
		lines = renderKV(m.curReq.Headers, height)
	case TabBody:
		body := strings.TrimRight(m.curReq.Body, "\n")
		if body == "" {
			lines = []string{tabInactive.Render("(empty — press e to edit in $EDITOR)")}
		} else {
			lines = truncateLines(strings.Split(body, "\n"), height)
		}
	}
	return strings.Join(lines, "\n")
}

// renderResponse 渲染右下响应面板：状态行 + body。
func (m Model) renderResponse() string {
	if m.sending {
		return titleStyle.Render("sending…")
	}
	if m.resp == nil {
		if m.err != nil {
			return errStyle.Render(m.err.Error())
		}
		return tabInactive.Render("(no response yet — press s to send)")
	}
	head := formatStatusLine(*m.resp)
	return lipgloss.JoinVertical(lipgloss.Left, head, m.viewport.View())
}

// renderBottom 渲染底部状态栏 / 命令面板。
func (m Model) renderBottom() string {
	if m.focus == FocusCommand {
		return m.palette.View()
	}
	left := m.statusMsg
	if m.err != nil {
		left = errStyle.Render(m.err.Error())
	}
	if m.dirty {
		left = dirtyMark.Render("* ") + left
	}
	hint := tabInactive.Render("s send  : cmd  ? help  q quit")
	return lipgloss.JoinHorizontal(lipgloss.Left, left, strings.Repeat(" ", max(0, m.width-len(left)-len(hint))), hint)
}

// helpView 渲染 ? 帮助覆盖层。
func (m Model) helpView() string {
	rows := []string{
		titleStyle.Render("tpost — key bindings"),
		"",
		"  j/k       上下移动",
		"  h/l       Panel 切换",
		"  Enter     打开 Request",
		"  Tab       切 Params → Headers → Body",
		"  e         Edit body ($EDITOR)",
		"  s/Ctrl+R  Send",
		"  Ctrl+S    Save",
		"  :         Command palette",
		"  q         Quit",
		"  ?         Toggle help",
		"",
		tabInactive.Render("press ? to close"),
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// formatStatusLine 把响应汇总成一行：200 OK  126ms  1.4KB
func formatStatusLine(r model.Response) string {
	style := okStyle
	if r.StatusCode >= 400 {
		style = errStyle
	}
	return fmt.Sprintf("%s  %s  %s",
		style.Render(r.Status),
		r.Latency.Round(0).String(),
		humanBytes(r.Size),
	)
}

func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/1024/1024)
	}
}

// ---- 渲染辅助 ----

func renderKV(m map[string]string, height int) []string {
	if len(m) == 0 {
		return []string{tabInactive.Render("(empty)")}
	}
	keys := sortedKeys(m)
	var lines []string
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s: %s", k, m[k]))
		if len(lines) >= height {
			break
		}
	}
	return truncateLines(lines, height)
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	// 简单排序
	for i := 0; i < len(ks); i++ {
		for j := i + 1; j < len(ks); j++ {
			if ks[i] > ks[j] {
				ks[i], ks[j] = ks[j], ks[i]
			}
		}
	}
	return ks
}

func truncateLines(lines []string, height int) []string {
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func truncate(s string, max int) string {
	if max <= 0 {
		return s
	}
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
