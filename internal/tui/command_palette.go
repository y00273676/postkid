package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/key"
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
	case "new", "export", "history", "import":
		return m.info(cmd + ": not implemented")
	}
	return m.info("unknown command: " + cmd)
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
		func() tea.Msg { return ResponseMsg{Resp: app.Send(resolved)} },
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
