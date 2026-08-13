package tui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

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
		m.closePalette()
		return m, nil
	case key.Matches(kmsg, enterKey):
		input := strings.TrimSpace(m.palette.Value())
		m.closePalette()
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
	args := parts[1:]
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}
	switch cmd {
	case "send":
		return m.sendCurrent()
	case "save":
		return m.saveCurrent()
	case "env":
		if arg == "use" {
			if len(args) < 2 {
				return m.info("usage: env use <name>")
			}
			name := strings.Join(args[1:], " ")
			if err := m.app.SetEnvironment(name); err != nil {
				return m.errorCmd(err)
			}
			return m.info("env switched to " + name)
		}
		if isWorkspaceAction(arg) {
			target := strings.Join(args[1:], " ")
			return m.openWorkspaceCommand("environment", arg, target)
		}
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
	case "collection", "collections":
		if arg == "" {
			return m.info("usage: collection new|rename|delete [name]")
		}
		if !isWorkspaceAction(arg) {
			return m.info("usage: collection new|rename|delete [name]")
		}
		return m.openWorkspaceCommand("collection", arg, strings.Join(args[1:], " "))
	case "environment", "environments":
		if arg == "" || !isWorkspaceAction(arg) {
			return m.info("usage: env new|rename|delete [name]")
		}
		return m.openWorkspaceCommand("environment", arg, strings.Join(args[1:], " "))
	case "import":
		if strings.ToLower(arg) != "curl" || len(args) != 1 {
			return m.info("usage: import curl")
		}
		return m.openCurlImportCommand()
	}
	return m.info("unknown command: " + cmd)
}

func (m Model) openCurlImportCommand() tea.Cmd {
	return func() tea.Msg { return CurlImportOpenMsg{} }
}

func isWorkspaceAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "new", "rename", "delete":
		return true
	default:
		return false
	}
}

// openWorkspaceCommand returns a message instead of mutating the model in a
// command closure. The next Update call creates the modal synchronously, so
// text inputs are ready before the next key event arrives.
func (m Model) openWorkspaceCommand(resource, action, target string) tea.Cmd {
	return func() tea.Msg {
		return WorkspaceModalMsg{Resource: resource, Action: action, Target: target}
	}
}

// exportCurl 把当前请求转为 curl 命令并复制到剪贴板。
func (m Model) exportCurl() tea.Cmd {
	if m.curReq == nil {
		return m.errorCmd(fmt.Errorf("no request selected"))
	}
	resolved, err := m.resolveCurrent()
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
	if m.curReq == nil {
		return m.errorCmd(fmt.Errorf("no request selected"))
	}
	resolved, err := m.resolveCurrent()
	if err != nil {
		return m.errorCmd(err)
	}
	app := m.app
	// Batch executes commands concurrently, so a fast response can arrive
	// before sendingMsg and leave the spinner stuck on. Sequence guarantees the
	// sending state is observed before the network command is started.
	return tea.Sequence(
		func() tea.Msg { return sendingMsg{} },
		func() tea.Msg {
			resp := app.Send(resolved)
			return ResponseMsg{Resp: resp, Resolved: resolved}
		},
	)
}

// saveCurrent 把当前请求回写到 collection 文件。
func (m Model) saveCurrent() tea.Cmd {
	if m.curReq == nil {
		return m.errorCmd(fmt.Errorf("no request selected"))
	}
	if m.curColl == nil {
		return m.errorCmd(fmt.Errorf("history requests cannot be saved directly; select a collection first"))
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

// resolveCurrent resolves both collection requests and requests loaded from
// history. A history snapshot has no collection-level variables, so an empty
// collection is the correct context for it.
func (m Model) resolveCurrent() (model.ResolvedRequest, error) {
	if m.curReq == nil {
		return model.ResolvedRequest{}, fmt.Errorf("no request selected")
	}
	if m.app == nil {
		return model.ResolvedRequest{}, fmt.Errorf("application unavailable")
	}
	var coll model.Collection
	if m.curColl != nil {
		coll = *m.curColl
	}
	return m.app.ResolveRequest(*m.curReq, coll)
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

// newRequestCmd asks Update to open the modal form.  The form must be opened
// by Update (rather than from inside a command closure) so its text inputs are
// part of the model before the next key event arrives.
func (m Model) newRequestCmd() tea.Cmd {
	return func() tea.Msg {
		return NewRequestMsg{}
	}
}
