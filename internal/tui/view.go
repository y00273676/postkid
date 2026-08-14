package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// View 渲染整个界面。
func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	if m.showHelp {
		return m.helpView()
	}
	if m.modal != nil {
		return m.modalView()
	}
	if m.workspaceModal != nil {
		return m.workspaceModalView()
	}
	if m.curlImport != nil {
		return m.curlImportView()
	}
	if m.grpcModal != nil {
		return m.grpcModalView()
	}

	listW := max(m.width*28/100, 24)
	rightW := m.width - listW
	contentH := m.height - 1 // 底部状态栏占 1 行
	reqH := max(contentH*45/100, 6)
	respH := max(contentH-reqH, 3)

	// 注意：lipgloss 的 Width/Height 含 padding 但不含 border，
	// 面板外宽 = Width + 2，文本可用宽 = Width - 2（横向 padding）。
	// 列表面板自带标题行（列表组件的 Title 已禁用，换取统一的标题样式）。
	listContent := panelTitle("Collections", listW-4, m.focus == FocusList) + "\n" + m.list.View()
	left := borderStyle(m.focus == FocusList).
		Width(listW - 2).Height(contentH - 2).
		Render(listContent)

	reqContent := m.renderRequest(rightW-4, reqH-2)
	reqBox := borderStyle(m.focus == FocusRequest).
		Width(rightW - 2).Height(reqH - 2).
		Render(reqContent)

	respContent := m.renderResponse()
	respBox := borderStyle(m.focus == FocusResponse).
		Width(rightW - 2).Height(respH - 2).
		Render(respContent)

	right := lipgloss.JoinVertical(lipgloss.Left, reqBox, respBox)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.renderBottom())
}

// modalView renders a centered, focused card. Forms intentionally replace the
// panel view while open: this gives text inputs enough room and makes the
// modal boundary obvious in a narrow terminal too.
func (m Model) modalView() string {
	cardWidth := m.width - 8
	if cardWidth < 36 {
		cardWidth = 36
	}
	if cardWidth > 86 {
		cardWidth = 86
	}
	card := borderStyle(true).
		Width(cardWidth).
		Padding(1, 2).
		Render(m.modalContent())
	screenWidth := m.width
	if screenWidth < lipgloss.Width(card)+2 {
		screenWidth = lipgloss.Width(card) + 2
	}
	screenHeight := m.height
	if screenHeight < lipgloss.Height(card)+2 {
		screenHeight = lipgloss.Height(card) + 2
	}
	return lipgloss.Place(screenWidth, screenHeight, lipgloss.Center, lipgloss.Center, card)
}

// panelTitle 渲染面板标题行：加粗标题 + 弱化分隔线补齐宽度。
func panelTitle(label string, width int, focused bool) string {
	style := panelTitleUnfocused
	if focused {
		style = panelTitleFocused
	}
	title := style.Render(label)
	return title + " " + rule(width-lipgloss.Width(title)-1)
}

// renderRequest 渲染右上请求面板：方法+URL、tab bar、当前 tab 内容。
func (m Model) renderRequest(width, height int) string {
	if m.curReq == nil {
		return emptyStyle.Render("No request selected")
	}
	if m.curReq.IsGRPC() {
		return m.renderGRPCRequest(width, height)
	}

	method := m.curReq.Method
	url := m.curReq.URL
	if m.app != nil {
		// 展示已解析的 URL（变量替换后），便于确认实际请求地址
		var coll model.Collection
		if m.curColl != nil {
			coll = *m.curColl
		}
		if r, err := m.app.ResolveRequest(*m.curReq, coll); err == nil {
			url = r.URL
		}
	}
	top := methodBadge(method) + " " + urlStyle.Render(truncate(url, width-10))

	tabs := m.renderTabs()
	content := m.renderTabContent(height - 3)

	return lipgloss.JoinVertical(lipgloss.Left, top, tabs, rule(width), content)
}

func (m Model) renderGRPCRequest(width, height int) string {
	target := m.curReq.URL
	service := grpcService(*m.curReq)
	method := grpcMethod(*m.curReq)
	tls := false
	if m.app != nil {
		var coll model.Collection
		if m.curColl != nil {
			coll = *m.curColl
		}
		if resolved, err := m.app.ResolveGRPCRequest(*m.curReq, coll); err == nil {
			target = resolved.Target
			service = resolved.Service
			method = resolved.Method
			tls = resolved.TLS.Enabled
		}
	}
	transport := "plaintext"
	if tls {
		transport = "TLS"
	}
	top := methodBadge("GRPC") + " " + urlStyle.Render(truncate(target, width-22)) + " " + mutedStyle.Render("["+transport+"]")
	call := strings.Trim(strings.TrimSpace(service)+"/"+strings.TrimSpace(method), "/")
	if call != "" {
		top += "\n" + keyStyle.Render("RPC") + punctStyle.Render(" : ") + valueStyle.Render(truncate(call, width-8))
	}
	tabs := m.renderTabs()
	content := m.renderTabContent(height - 4)
	return lipgloss.JoinVertical(lipgloss.Left, top, tabs, rule(width), content)
}

func (m Model) renderTabs() string {
	names := []string{"Params", "Headers", "Body", "Auth"}
	if m.curReq != nil && m.curReq.IsGRPC() {
		names = []string{"RPC", "Metadata", "Body", "TLS"}
	}
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
		if m.curReq.IsGRPC() {
			lines = []string{
				keyStyle.Render("Service") + punctStyle.Render(" : ") + valueStyle.Render(grpcService(*m.curReq)),
				keyStyle.Render("Method") + punctStyle.Render(" : ") + valueStyle.Render(grpcMethod(*m.curReq)),
				mutedStyle.Render("press e to edit or :grpc discover to use reflection"),
			}
		} else {
			lines = renderKV(m.curReq.Params, height)
		}
	case TabHeaders:
		values := m.curReq.Headers
		if m.curReq.IsGRPC() {
			values = grpcMetadata(m.curReq)
		}
		lines = renderKV(values, height)
	case TabBody:
		body := strings.TrimRight(m.curReq.Body, "\n")
		if body == "" {
			lines = []string{emptyStyle.Render("empty — press e to edit in $EDITOR")}
		} else {
			lines = truncateLines(strings.Split(HighlightJSON(body), "\n"), height)
		}
	case TabAuth:
		if m.curReq.IsGRPC() {
			transport := "plaintext"
			serverName := ""
			verify := "verify certificate"
			if m.curReq.GRPC != nil && m.curReq.GRPC.TLS != nil && m.curReq.GRPC.TLS.Enabled {
				transport = "TLS"
				serverName = m.curReq.GRPC.TLS.ServerName
				if m.curReq.GRPC.TLS.InsecureSkipVerify {
					verify = "skip certificate verification"
				}
			}
			lines = []string{
				keyStyle.Render("Transport") + punctStyle.Render(" : ") + valueStyle.Render(transport),
				keyStyle.Render("Server name") + punctStyle.Render(" : ") + valueStyle.Render(serverName),
				keyStyle.Render("Verification") + punctStyle.Render(" : ") + valueStyle.Render(verify),
			}
		} else {
			lines = renderAuth(m.curReq, height)
		}
	}
	return strings.Join(lines, "\n")
}

// renderResponse 渲染右下响应面板：历史列表 / 状态行 + 分隔线 + body。
func (m Model) renderResponse() string {
	if m.showHistory {
		return m.renderHistory()
	}
	if m.sending {
		return lipgloss.NewStyle().Foreground(colAccent).Render(m.spinner.View() + " sending…")
	}
	if m.grpcResp != nil {
		return m.renderGRPCResponse()
	}
	if m.resp == nil {
		if m.err != nil {
			return errStyle.Render(m.err.Error())
		}
		return emptyStyle.Render("no response yet — press s to send")
	}
	head := renderStatusHead(*m.resp)
	return lipgloss.JoinVertical(lipgloss.Left, head, rule(m.viewport.Width), m.viewport.View())
}

func (m Model) renderGRPCResponse() string {
	if m.grpcResp == nil {
		return emptyStyle.Render("no gRPC response yet — press s to send")
	}
	r := *m.grpcResp
	head := renderGRPCStatusHead(r)
	if r.Err != nil {
		return lipgloss.JoinVertical(lipgloss.Left, head, rule(m.viewport.Width), errStyle.Render(r.Err.Error()))
	}
	return lipgloss.JoinVertical(lipgloss.Left, head, rule(m.viewport.Width), m.viewport.View())
}

func renderGRPCStatusHead(r model.GRPCResponse) string {
	status := r.Status
	if status == "" {
		status = "UNKNOWN"
	}
	badge := statusBadge("gRPC "+status, 0)
	if r.Err != nil {
		badge = errStyle.Render("gRPC " + status)
	}
	return badge + "  " + mutedStyle.Render(fmt.Sprintf("%s  %s", r.Latency.Round(0), humanBytes(r.Size)))
}

func formatGRPCStatusLine(r model.GRPCResponse) string {
	status := r.Status
	if status == "" {
		status = "UNKNOWN"
	}
	style := okStyle
	if r.Err != nil {
		style = errStyle
	}
	return fmt.Sprintf("%s  %s  %s", style.Render("gRPC "+status), r.Latency.Round(0), humanBytes(r.Size))
}

// renderStatusHead 响应面板顶行：状态徽章 + 延迟/大小。
func renderStatusHead(r model.Response) string {
	meta := mutedStyle.Render(fmt.Sprintf("%s  %s",
		r.Latency.Round(0).String(), humanBytes(r.Size)))
	truncated := ""
	if r.Truncated {
		truncated = "  " + truncatedStyle.Render("truncated")
	}
	return statusBadge(r.Status, r.StatusCode) + "  " + meta + truncated
}

// renderBottom 渲染底部状态栏 / 命令面板 / 搜索框。
// 所有分段自带底色，避免外层包裹时 ANSI reset 破坏背景。
func (m Model) renderBottom() string {
	if m.focus == FocusCommand {
		return m.barLine(m.palette.View())
	}
	if m.searching {
		return m.barLine(m.searchInput.View())
	}

	brand := brandStyle.Render("postkid")
	var msg string
	if m.err != nil {
		msg = barErrStyle.Render(" " + m.err.Error())
	} else {
		msg = statusBarStyle.Render(" " + m.statusMsg)
	}
	if m.dirty {
		msg = barDirtyStyle.Render(" ●") + msg
	}
	left := brand + msg

	hint := renderHints([][2]string{
		{"s", "send"}, {"^s", "save"}, {"e", "edit"}, {"m", "url"}, {":", "cmd"}, {"?", "help"}, {"q", "quit"},
	})
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(hint)
	return left + barGap(gap) + hint
}

// barLine 把命令面板/搜索输入行补齐成整行状态栏。
func (m Model) barLine(content string) string {
	return content + barGap(m.width-lipgloss.Width(content))
}

func renderHints(pairs [][2]string) string {
	var parts []string
	for _, p := range pairs {
		parts = append(parts, hintKeyStyle.Render(p[0])+hintDescStyle.Render(" "+p[1]))
	}
	return strings.Join(parts, hintSepStyle.Render(" · "))
}

// helpView 渲染 ? 帮助：居中卡片，按键分组。
func (m Model) helpView() string {
	groups := []struct {
		name  string
		binds [][2]string
	}{
		{"导航", [][2]string{
			{"j / k", "上下移动"},
			{"h / l", "Panel 切换"},
			{"Enter", "打开请求"},
			{"/", "搜索"},
			{"Tab", "切 Params → Headers → Body → Auth"},
		}},
		{"请求", [][2]string{
			{"s / ^r", "发送"},
			{"^s", "保存"},
			{"e", "编辑当前 tab（Body 用 $EDITOR）"},
			{"m", "编辑 Method / URL"},
			{"n", "新建请求"},
			{"d", "删除请求"},
		}},
		{"通用", [][2]string{
			{":", "命令面板"},
			{"collection new", "新建 Collection"},
			{"collection rename/delete", "管理 Collection"},
			{"env new/rename/delete", "管理 Environment"},
			{"import curl", "导入 cURL"},
			{"import postman <path>", "导入 Postman Collection 文件"},
			{"import postman-env <path>", "导入并切换 Postman Environment"},
			{"?", "帮助开关"},
			{"q", "退出"},
		}},
	}

	var rows []string
	rows = append(rows, titleStyle.Render("postkid — key bindings"))
	for _, g := range groups {
		rows = append(rows, "", keyStyle.Render(g.name))
		for _, b := range g.binds {
			rows = append(rows, fmt.Sprintf("  %s  %s",
				lipgloss.NewStyle().Foreground(colAccent).Width(8).Render(b[0]),
				mutedStyle.Render(b[1])))
		}
	}
	rows = append(rows, "", mutedStyle.Render("press ? to close"))

	card := borderStyle(true).Padding(0, 2).Render(
		lipgloss.JoinVertical(lipgloss.Left, rows...))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

// formatStatusLine 把响应汇总成一行：200 OK  126ms  1.4KB（状态栏用，无徽章底色）
func formatStatusLine(r model.Response) string {
	style := okStyle
	if r.StatusCode >= 400 {
		style = errStyle
	}
	line := fmt.Sprintf("%s  %s  %s",
		style.Render(r.Status),
		r.Latency.Round(0).String(),
		humanBytes(r.Size),
	)
	if r.Truncated {
		line += "  " + truncatedStyle.Render("truncated")
	}
	return line
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

// renderKV 渲染 key: value 列表，key 列对齐并上色。
func renderKV(m map[string]string, height int) []string {
	if len(m) == 0 {
		return []string{mutedStyle.Render("(empty)")}
	}
	keys := sortedKeys(m)
	maxK := 0
	for _, k := range keys {
		if len(k) > maxK {
			maxK = len(k)
		}
	}
	var lines []string
	for _, k := range keys {
		lines = append(lines,
			keyStyle.Render(fmt.Sprintf("%-*s", maxK, k))+
				punctStyle.Render(" : ")+
				valueStyle.Render(m[k]))
		if len(lines) >= height {
			break
		}
	}
	return truncateLines(lines, height)
}

func renderAuth(req *model.Request, height int) []string {
	_ = height
	switch req.AuthType {
	case model.AuthBasic:
		return []string{
			keyStyle.Render("Basic Auth"),
			"",
			keyStyle.Render("Username") + punctStyle.Render(" : ") + valueStyle.Render(req.AuthUsername),
			keyStyle.Render("Password") + punctStyle.Render(" : ") + mutedStyle.Render(maskSecret(req.AuthPassword)),
		}
	case model.AuthBearer:
		return []string{
			keyStyle.Render("Bearer Token"),
			"",
			keyStyle.Render("Token") + punctStyle.Render(" : ") + mutedStyle.Render(maskSecret(req.AuthToken)),
		}
	default:
		return []string{mutedStyle.Render("(none — edit YAML to set auth_type)")}
	}
}

func maskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
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

// renderHistory 渲染历史记录列表。
func (m Model) renderHistory() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("History"))
	b.WriteString("\n\n")
	for i, entry := range m.historyEntries {
		status := mutedStyle.Render(" --- ")
		if entry.Response.StatusCode > 0 {
			status = statusBadge(entry.Response.Status, entry.Response.StatusCode)
		}
		line := fmt.Sprintf("%s %s %s",
			methodBadge(entry.Request.Method),
			truncate(entry.Request.URL, 50),
			status)
		meta := mutedStyle.Render(fmt.Sprintf("         %s  %s",
			relativeTime(entry.Timestamp), entry.Response.Latency))
		if i == m.historyIdx {
			line = selectedRowStyle.Render("▎" + line)
		} else {
			line = " " + line
		}
		b.WriteString(line)
		b.WriteString("\n")
		b.WriteString(meta)
		b.WriteString("\n")
	}
	return b.String()
}

// relativeTime 返回一个人类可读的过去时间，如 "2m ago"、"1h ago"。
func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
