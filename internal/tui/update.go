package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/key"

	"go.planetmeican.com/yangguang/postkid/internal/editor"
)

// Update 路由所有消息。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case ResponseMsg:
		m.sending = false
		r := msg.Resp
		if r.Err != nil {
			m.err = r.Err
			m.statusMsg = ""
			return m, nil
		}
		m.resp = &r
		m.err = nil
		m.viewport.SetContent(r.Body)
		m.viewport.GotoTop()
		m.statusMsg = formatStatusLine(r)
		return m, nil

	case editor.DoneMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		if m.curReq != nil {
			m.curReq.Body = msg.Body
			m.dirty = true
			m.statusMsg = "body edited — Ctrl+S to save"
		}
		return m, nil

	case ErrorMsg:
		m.err = msg.Err
		return m, nil

	case InfoMsg:
		m.statusMsg = msg.Text
		m.err = nil
		return m, nil

	case sendingMsg:
		m.sending = true
		m.err = nil
		m.statusMsg = "sending…"
		return m, nil

	case savedMsg:
		m.dirty = false
		m.statusMsg = "saved"
		return m, nil
	}

	// 命令面板模式下，所有键先交给 palette
	if m.focus == FocusCommand {
		return m.updatePalette(msg)
	}

	// 键盘消息
	kmsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	// 全局键
	switch {
	case key.Matches(kmsg, keys.Quit):
		return m, tea.Quit
	case key.Matches(kmsg, keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(kmsg, keys.Command):
		m.focus = FocusCommand
		m.palette.Reset()
		m.palette.Focus()
		return m, nil
	case key.Matches(kmsg, keys.Send):
		return m, m.sendCurrent()
	case key.Matches(kmsg, keys.Save):
		return m, m.saveCurrent()
	}

	// 焦点切换
	if key.Matches(kmsg, keys.Left) || key.Matches(kmsg, keys.Right) {
		m.moveFocus(kmsg)
		return m, nil
	}

	// 按当前焦点分发
	switch m.focus {
	case FocusList:
		return m.updateList(kmsg)
	case FocusRequest:
		return m.updateRequest(kmsg)
	case FocusResponse:
		return m.updateResponse(kmsg)
	}
	return m, nil
}

// moveFocus 用 h/l 在 List ↔ Request ↔ Response 间循环。
func (m *Model) moveFocus(msg tea.KeyMsg) {
	if key.Matches(msg, keys.Right) {
		switch m.focus {
		case FocusList:
			m.focus = FocusRequest
		case FocusRequest:
			m.focus = FocusResponse
		case FocusResponse:
			m.focus = FocusList
		}
	} else {
		switch m.focus {
		case FocusList:
			m.focus = FocusResponse
		case FocusRequest:
			m.focus = FocusList
		case FocusResponse:
			m.focus = FocusRequest
		}
	}
}

// updateList 处理左侧列表的按键。
func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Up), key.Matches(msg, keys.Down):
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		m.selectCurrent()
		return m, cmd
	case key.Matches(msg, enterKey):
		m.selectCurrent()
		if m.curReq != nil {
			m.focus = FocusRequest
		}
		return m, nil
	}
	// 其余键交给 list
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	m.selectCurrent()
	return m, cmd
}

// updateRequest 处理请求面板的按键。
func (m Model) updateRequest(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.NextTab):
		m.tab = (m.tab + 1) % 3
		return m, nil
	case key.Matches(msg, keys.PrevTab):
		m.tab = (m.tab + 2) % 3
		return m, nil
	case key.Matches(msg, keys.EditBody):
		if m.curReq != nil {
			m.tab = TabBody
			return m, editor.Open(m.curReq.Body)
		}
	}
	return m, nil
}

// updateResponse 处理响应面板的按键（j/k 滚动）。
func (m Model) updateResponse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// selectCurrent 把 list 选中项同步到 curColl/curReq。
func (m *Model) selectCurrent() {
	item, ok := m.list.SelectedItem().(listItem)
	if !ok {
		m.curColl, m.curReq = nil, nil
		return
	}
	m.curColl, m.curReq = item.coll, item.req
	m.viewport.GotoTop()
}

// resize 在窗口尺寸变化时调整各组件。
func (m *Model) resize() {
	listW := max(m.width*28/100, 24)
	rightW := max(m.width-listW-2, 10) // 留边框
	reqH := max(m.height*45/100, 6)
	respH := max(m.height-reqH-4, 3) // 留 tab + 状态栏
	m.list.SetSize(listW, m.height-2)
	m.viewport.Width = rightW
	m.viewport.Height = respH
}
