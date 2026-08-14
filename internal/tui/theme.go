package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Tokyo Night (Storm) 色板 —— 全部样式收口在本文件。
var (
	colAccent      = lipgloss.Color("#7aa2f7") // 蓝：聚焦边框、活动 tab、键位
	colAccentAlt   = lipgloss.Color("#bb9af7") // 紫：KV key、标题点缀
	colCyan        = lipgloss.Color("#7dcfff") // URL、字符串值
	colText        = lipgloss.Color("#c0caf5") // 正文
	colMuted       = lipgloss.Color("#565f89") // 次要信息
	colFaint       = lipgloss.Color("#3b4261") // 分隔线、未聚焦边框
	colBg          = lipgloss.Color("#1f2335") // 状态栏底
	colBgHighlight = lipgloss.Color("#2f3549") // 选中行
	colFgDark      = lipgloss.Color("#1a1b26") // 胶囊上的深色文字
	colGreen       = lipgloss.Color("#9ece6a")
	colOrange      = lipgloss.Color("#ff9e64")
	colRed         = lipgloss.Color("#f7768e")
	colYellow      = lipgloss.Color("#e0af68")
)

var (
	promptStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)

	// 面板标题：聚焦时强调色，未聚焦时弱化为次要信息色
	panelTitleFocused   = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	panelTitleUnfocused = lipgloss.NewStyle().Bold(true).Foreground(colMuted)
	dividerStyle        = lipgloss.NewStyle().Foreground(colFaint)

	// Tab：激活为实心胶囊，未激活为灰字
	tabActive   = lipgloss.NewStyle().Background(colAccent).Foreground(colFgDark).Bold(true).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Foreground(colMuted).Padding(0, 1)

	keyStyle   = lipgloss.NewStyle().Foreground(colAccentAlt)
	valueStyle = lipgloss.NewStyle().Foreground(colText)
	punctStyle = lipgloss.NewStyle().Foreground(colFaint)

	dirtyMark      = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	errStyle       = lipgloss.NewStyle().Foreground(colRed)
	okStyle        = lipgloss.NewStyle().Foreground(colGreen)
	truncatedStyle = lipgloss.NewStyle().Foreground(colYellow).Bold(true)

	urlStyle   = lipgloss.NewStyle().Foreground(colCyan)
	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)
	emptyStyle = lipgloss.NewStyle().Foreground(colMuted).Italic(true)

	brandStyle     = lipgloss.NewStyle().Background(colAccent).Foreground(colFgDark).Bold(true).Padding(0, 1)
	statusBarStyle = lipgloss.NewStyle().Background(colBg).Foreground(colText)
	barErrStyle    = lipgloss.NewStyle().Background(colBg).Foreground(colRed)
	barDirtyStyle  = lipgloss.NewStyle().Background(colBg).Foreground(colRed).Bold(true)
	hintKeyStyle   = lipgloss.NewStyle().Background(colBg).Foreground(colAccent).Bold(true)
	hintDescStyle  = lipgloss.NewStyle().Background(colBg).Foreground(colMuted)
	hintSepStyle   = lipgloss.NewStyle().Background(colBg).Foreground(colFaint)

	selectedRowStyle = lipgloss.NewStyle().Background(colBgHighlight).Foreground(colText)
)

// barGap 返回带状态栏底色的空格填充。
func barGap(width int) string {
	if width <= 0 {
		return ""
	}
	return statusBarStyle.Render(strings.Repeat(" ", width))
}

// rule 返回弱化色的水平分隔线。
func rule(width int) string {
	if width <= 0 {
		return ""
	}
	return dividerStyle.Render(strings.Repeat("─", width))
}

// methodColor 返回 HTTP 方法的语义色（API 工具惯例）。
func methodColor(method string) lipgloss.Color {
	switch method {
	case "GET":
		return colGreen
	case "POST":
		return colYellow
	case "PUT":
		return colAccent
	case "PATCH":
		return colAccentAlt
	case "DELETE":
		return colRed
	case "GRPC":
		return colAccentAlt
	default:
		return colMuted
	}
}

// methodBadge 把方法名渲染成定宽胶囊，如 "  GET  "。
func methodBadge(method string) string {
	return lipgloss.NewStyle().
		Background(methodColor(method)).Foreground(colFgDark).Bold(true).
		Width(7).Align(lipgloss.Center).
		Render(method)
}

// statusBadge 把状态码渲染成色块胶囊：2xx 绿 / 3xx 蓝 / 4xx 黄 / 5xx 红。
func statusBadge(status string, code int) string {
	bg := colGreen
	switch {
	case code >= 500:
		bg = colRed
	case code >= 400:
		bg = colOrange
	case code >= 300:
		bg = colAccent
	}
	return lipgloss.NewStyle().
		Background(bg).Foreground(colFgDark).Bold(true).
		Padding(0, 1).
		Render(status)
}

func borderStyle(focused bool) lipgloss.Style {
	fg := colFaint
	if focused {
		fg = colAccent
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(fg).Padding(0, 1)
}
