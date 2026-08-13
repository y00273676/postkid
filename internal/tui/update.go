package tui

import (
	"strconv"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"go.planetmeican.com/yangguang/postkid/internal/editor"
	"go.planetmeican.com/yangguang/postkid/internal/model"
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
		// 记录历史（无论成功还是失败）
		m.app.RecordHistory(msg.Resolved, r)
		if r.Err != nil {
			m.err = r.Err
			m.statusMsg = ""
			return m, nil
		}
		m.resp = &r
		m.err = nil
		m.viewport.SetContent(HighlightJSON(r.Body))
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
		return m, m.spinner.Tick

	case savedMsg:
		m.dirty = false
		m.statusMsg = "saved"
		return m, nil

	case HistoryMsg:
		m.showHistory = true
		m.historyEntries = msg.Entries
		m.historyIdx = 0
		m.statusMsg = "showing " + strconv.Itoa(len(msg.Entries)) + " history entries — Enter to load, Esc to close"
		return m, nil

	case ListUpdatedMsg:
		m.rebuildList()
		m.selectCurrent()
		m.statusMsg = "list updated"
		return m, nil

	case SearchModeMsg:
		if msg.Active {
			m.searchInput = textinput.New()
			m.searchInput.Prompt = "/"
			m.searchInput.Focus()
		} else {
			m.searchInput = textinput.Model{}
		}
		return m, nil
	}

	// 命令面板模式下，所有键先交给 palette
	if m.focus == FocusCommand {
		return m.updatePalette(msg)
	}

	// 发送中：spinner tick 驱动动画（ResponseMsg 已在前面的 switch 处理）
	if m.sending {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
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

	// 搜索模式：Esc 退出，其余键交给搜索输入框
	if m.searching {
		return m.updateSearch(kmsg)
	}

	// 历史浏览模式（优先于焦点切换和面板分发）
	if m.showHistory {
		return m.updateHistory(kmsg)
	}

	// 焦点切换（h/l）
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
	case key.Matches(msg, keys.New):
		return m, m.newRequestCmd()
	case key.Matches(msg, keys.Delete):
		return m, m.deleteCurrentCmd()
	case key.Matches(msg, keys.Search):
		m.searching = true
		m.searchInput = textinput.New()
		m.searchInput.Prompt = "/"
		m.searchInput.Focus()
		return m, nil
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
		m.tab = (m.tab + 1) % 4
		return m, nil
	case key.Matches(msg, keys.PrevTab):
		m.tab = (m.tab + 3) % 4
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

// updateHistory 处理历史浏览模式的按键。
func (m Model) updateHistory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Up):
		if m.historyIdx > 0 {
			m.historyIdx--
		}
		return m, nil
	case key.Matches(msg, keys.Down):
		if m.historyIdx < len(m.historyEntries)-1 {
			m.historyIdx++
		}
		return m, nil
	case key.Matches(msg, enterKey):
		entry := m.historyEntries[m.historyIdx]
		m.showHistory = false
		// 将历史记录加载为当前请求
		req := &model.Request{
			Name:    "history",
			Method:  entry.Request.Method,
			URL:     entry.Request.URL,
			Headers: entry.Request.Headers,
			Body:    entry.Request.Body,
		}
		// 从历史记录中还原请求，按历史设定
		m.curReq = req
		m.curColl = nil // 历史记录不属于任何 collection
		m.focus = FocusRequest
		m.tab = TabBody
		m.statusMsg = "loaded from history — save to collection to persist"
		return m, nil
	case key.Matches(msg, keys.Quit), key.Matches(msg, keys.Back):
		m.showHistory = false
		m.statusMsg = ""
		return m, nil
	}
	return m, nil
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
// 各面板外框占 2（边框）+ 2（横向 padding），内部组件按内容盒尺寸设置。
func (m *Model) resize() {
	listW := max(m.width*28/100, 24)
	rightW := max(m.width-listW, 10)
	contentH := m.height - 1
	reqH := max(contentH*45/100, 6)
	respH := max(contentH-reqH, 3)
	m.list.SetSize(listW-4, contentH-4)
	m.viewport.Width = rightW - 4
	m.viewport.Height = respH - 3 // 边框 2 + 状态行 1
}

// updateSearch 处理搜索模式的按键。
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.searching = false
		m.searchInput = textinput.Model{}
		m.list.SetItems(m.allItems())
		m.statusMsg = ""
		return m, nil
	case key.Matches(msg, enterKey):
		m.searching = false
		query := m.searchInput.Value()
		if query == "" {
			m.list.SetItems(m.allItems())
		} else {
			m.list.SetItems(m.filterItems(query))
		}
		m.statusMsg = "search: " + query
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	// 实时过滤
	query := m.searchInput.Value()
	if query == "" {
		m.list.SetItems(m.allItems())
	} else {
		m.list.SetItems(m.filterItems(query))
	}
	return m, cmd
}
