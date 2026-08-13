package tui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/key"

	"go.planetmeican.com/yangguang/postkid/internal/app"
	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// updatePalette 处理命令面板模式下的所有消息。
func (m Model) updatePalette(msg tea.Msg) (tea.Model, tea.Cmd) {
	kmsg, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.palette, cmd = m.palette.Update(msg)
		return m, cmd
	}
	switch {
	case key.Matches(kmsg, keys.Back):
		m.focus = FocusList
		m.palette.Blur()
		return m, nil
	case key.Matches(kmsg, enterKey):
		input := strings.TrimSpace(m.palette.Value())
		m.focus = FocusList
		m.palette.Blur()
		if input == "" {
			return m, nil
		}
		return m, m.executeCommand(input)
	}
	var cmd tea.Cmd
	m.palette, cmd = m.palette.Update(msg)
	return m, cmd
}

// executeCommand 解析命令并分发。
func (m Model) executeCommand(input string) tea.Cmd {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}
	cmd := parts[0]
	arg := ""
	if len(parts) > 1 {
		arg = parts[1]
	}
	switch cmd {
	case "send":
		return m.sendCurrent()
	case "save":
		return m.saveCurrent()
	case "env":
		if arg == "" {
			return m.info("usage: env <name>")
		}
		if err := m.app.SetEnvironment(arg); err != nil {
			return m.errorCmd(err)
		}
		return m.info("env switched to " + arg)
	case "export":
		if arg != "curl" {
			return m.info("usage: export curl")
		}
		return m.exportCurl()
	case "history":
		return m.showHistoryCmd()
	case "new":
		return m.newRequestCmd()
	case "import":
		return m.info(cmd + ": not implemented")
	}
	return m.info("unknown command: " + cmd)
}

// exportCurl 把当前请求转为 curl 命令并复制到剪贴板。
func (m Model) exportCurl() tea.Cmd {
	if m.curReq == nil || m.curColl == nil {
		return m.errorCmd(fmt.Errorf("no request selected"))
	}
	req := *m.curReq
	coll := *m.curColl
	resolved, err := m.app.ResolveRequest(req, coll)
	if err != nil {
		return m.errorCmd(err)
	}
	curl := app.ExportCurl(resolved)
	if err := clipboard.WriteAll(curl); err != nil {
		// 剪贴板不可用时，在状态栏显示 curl 命令
		return m.info("curl: " + curl)
	}
	return m.info("curl command copied to clipboard")
}

// sendCurrent 解析并发送当前请求，异步返回 ResponseMsg。
func (m Model) sendCurrent() tea.Cmd {
	if m.curReq == nil || m.curColl == nil {
		return m.errorCmd(fmt.Errorf("no request selected"))
	}
	req := *m.curReq
	coll := *m.curColl
	resolved, err := m.app.ResolveRequest(req, coll)
	if err != nil {
		return m.errorCmd(err)
	}
	app := m.app
	return tea.Batch(
		func() tea.Msg { return sendingMsg{} },
		func() tea.Msg {
			resp := app.Send(resolved)
			return ResponseMsg{Resp: resp, Resolved: resolved}
		},
	)
}

// saveCurrent 把当前请求回写到 collection 文件。
func (m Model) saveCurrent() tea.Cmd {
	if m.curReq == nil || m.curColl == nil {
		return m.errorCmd(fmt.Errorf("no request selected"))
	}
	coll := m.curColl
	req := *m.curReq
	app := m.app
	return func() tea.Msg {
		if err := app.SaveRequest(coll, &req); err != nil {
			return ErrorMsg{Err: err}
		}
		return savedMsg{}
	}
}

func (m Model) info(text string) tea.Cmd {
	return func() tea.Msg { return InfoMsg{Text: text} }
}

func (m Model) errorCmd(err error) tea.Cmd {
	return func() tea.Msg { return ErrorMsg{Err: err} }
}

// showHistoryCmd 加载历史记录并切换到历史浏览模式。
func (m Model) showHistoryCmd() tea.Cmd {
	entries, err := m.app.LoadHistory()
	if err != nil {
		return m.errorCmd(err)
	}
	if len(entries) == 0 {
		return m.info("no history yet")
	}
	// 返回一个消息让 Update 处理
	return func() tea.Msg {
		return HistoryMsg{Entries: entries}
	}
}

// newRequestCmd 在当前 collection 中创建一个新请求并保存。
func (m Model) newRequestCmd() tea.Cmd {
	if m.curColl == nil {
		return m.errorCmd(fmt.Errorf("no collection selected"))
	}
	coll := m.curColl
	req := model.Request{
		Name:   "new-request",
		Method: "GET",
		URL:    "https://",
	}
	app := m.app
	return func() tea.Msg {
		if err := app.AddRequest(coll, &req); err != nil {
			return ErrorMsg{Err: err}
		}
		return ListUpdatedMsg{}
	}
}

// deleteCurrentCmd 删除当前选中的请求。
func (m Model) deleteCurrentCmd() tea.Cmd {
	if m.curReq == nil || m.curColl == nil {
		return m.errorCmd(fmt.Errorf("no request selected"))
	}
	name := m.curReq.Name
	coll := m.curColl
	app := m.app
	return func() tea.Msg {
		if err := app.DeleteRequest(coll, name); err != nil {
			return ErrorMsg{Err: err}
		}
		return ListUpdatedMsg{}
	}
}
