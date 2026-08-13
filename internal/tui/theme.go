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
	colMuted       = lipgloss.Color("#565f89") // 次要信息、未聚焦边框
	colBg          = lipgloss.Color("#24283b") // 状态栏底
	colBgHighlight = lipgloss.Color("#292e42") // 选中行
	colGreen       = lipgloss.Color("#9ece6a")
	colOrange      = lipgloss.Color("#ff9e64")
	colRed         = lipgloss.Color("#f7768e")
	colYellow      = lipgloss.Color("#e0af68")
)

var (
	promptStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)

	tabActive   = lipgloss.NewStyle().Bold(true).Foreground(colAccent).Underline(true)
	tabInactive = lipgloss.NewStyle().Foreground(colMuted)

	keyStyle   = lipgloss.NewStyle().Foreground(colAccentAlt)
	valueStyle = lipgloss.NewStyle().Foreground(colText)
	punctStyle = lipgloss.NewStyle().Foreground(colMuted)

	dirtyMark = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	errStyle  = lipgloss.NewStyle().Foreground(colRed)
	okStyle   = lipgloss.NewStyle().Foreground(colGreen)

	urlStyle   = lipgloss.NewStyle().Foreground(colCyan)
	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)

	statusBarStyle = lipgloss.NewStyle().Background(colBg).Foreground(colText)
	barErrStyle    = lipgloss.NewStyle().Background(colBg).Foreground(colRed)
	barDirtyStyle  = lipgloss.NewStyle().Background(colBg).Foreground(colRed).Bold(true)
	hintKeyStyle   = lipgloss.NewStyle().Background(colBg).Foreground(colAccent).Bold(true)
	hintDescStyle  = lipgloss.NewStyle().Background(colBg).Foreground(colMuted)
	hintSepStyle   = lipgloss.NewStyle().Background(colBg).Foreground(colMuted)

	selectedRowStyle = lipgloss.NewStyle().Background(colBgHighlight).Foreground(colText)
)

// barGap 返回带状态栏底色的空格填充。
func barGap(width int) string {
	if width <= 0 {
		return ""
	}
	return statusBarStyle.Render(strings.Repeat(" ", width))
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
	default:
		return colMuted
	}
}

// methodBadge 渲染彩色定宽方法名，如 "GET   "、"POST  "。
func methodBadge(method string) string {
	return lipgloss.NewStyle().
		Foreground(methodColor(method)).Bold(true).
		Width(6).
		Render(method)
}

// statusBadge 把状态码渲染成色块徽章：2xx 绿 / 3xx 蓝 / 4xx 黄 / 5xx 红。
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
		Background(bg).Foreground(colBg).Bold(true).
		Padding(0, 1).
		Render(status)
}

func borderStyle(focused bool) lipgloss.Style {
	fg := colMuted
	if focused {
		fg = colAccent
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(fg).Padding(0, 1)
}
