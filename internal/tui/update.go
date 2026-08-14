package tui

import (
	"fmt"
	"strconv"
	"strings"

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
		m.grpcResp = nil
		r := msg.Resp
		// 记录历史（无论成功还是失败）
		m.app.RecordHistory(msg.Resolved, r)
		if r.Err != nil {
			m.err = r.Err
			// Do not leave a previous successful response visible after a
			// failed request. The stale body is especially confusing when the
			// status line has already changed to an error.
			m.resp = nil
			m.viewport.SetContent("")
			m.statusMsg = ""
			return m, nil
		}
		m.resp = &r
		m.err = nil
		m.setResponseViewportContent(true)
		m.viewport.GotoTop()
		m.statusMsg = formatStatusLine(r)
		return m, nil

	case GRPCResponseMsg:
		m.sending = false
		r := msg.Resp
		m.grpcResp = &r
		m.resp = nil
		m.setResponseViewportContent(true)
		if r.Err != nil {
			m.err = r.Err
			m.statusMsg = ""
			return m, nil
		}
		m.err = nil
		m.viewport.GotoTop()
		m.statusMsg = formatGRPCStatusLine(r)
		return m, nil

	case editor.DoneMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		if m.curReq != nil {
			m.curReq.Body = msg.Body
			m.dirty = true
			if m.curColl == nil {
				m.statusMsg = "body edited — s to resend; history snapshot cannot be saved directly"
			} else {
				m.statusMsg = "body edited — Ctrl+S to save"
			}
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
		m.searching = msg.Active
		if msg.Active {
			m.searchInput = textinput.New()
			m.searchInput.Prompt = "/"
			m.searchInput.Focus()
		} else {
			m.searchInput = textinput.Model{}
			m.list.SetItems(m.allItems())
			m.list.ResetSelected()
			m.selectCurrent()
		}
		return m, nil

	case NewRequestMsg:
		m.openNewRequestModal()
		return m, nil

	case WorkspaceModalMsg:
		m.openWorkspaceModal(msg)
		return m, nil

	case WorkspaceUpdatedMsg:
		if msg.Resource == "collection" {
			m.rebuildList()
			m.list.ResetSelected()
			m.selectCurrent()
		}
		m.err = nil
		m.statusMsg = msg.Resource + " " + msg.Action + ": " + msg.Name
		return m, nil

	case CurlImportOpenMsg:
		m.openCurlImportModal()
		return m, nil

	case CurlImportSavedMsg:
		m.curlImport = nil
		m.rebuildList()
		m.list.ResetSelected()
		m.selectCurrent()
		m.err = nil
		m.statusMsg = "imported " + msg.Collection + "/" + msg.Name
		return m, nil

	case CurlImportSaveFailedMsg:
		if m.curlImport != nil {
			m.curlImport.phase = curlImportTarget
			if msg.Err != nil {
				m.curlImport.saveErr = msg.Err.Error()
			} else {
				m.curlImport.saveErr = "save failed"
			}
			m.err = nil
			return m, nil
		}
		m.err = msg.Err
		return m, nil

	case PostmanImportSavedMsg:
		m.rebuildList()
		m.list.ResetSelected()
		for index, item := range m.list.Items() {
			entry, ok := item.(listItem)
			if ok && entry.coll != nil && entry.coll.Name == msg.Collection {
				m.list.Select(index)
				break
			}
		}
		m.selectCurrent()
		m.err = nil
		m.statusMsg = fmt.Sprintf("imported %s (%d requests)", msg.Collection, msg.Imported)
		return m, nil

	case PostmanImportSaveFailedMsg:
		if msg.Err == nil {
			m.err = fmt.Errorf("postman import failed")
		} else {
			m.err = msg.Err
		}
		return m, nil

	case PostmanEnvironmentImportSavedMsg:
		m.err = nil
		m.statusMsg = fmt.Sprintf("imported environment %s (%d variables)", msg.Environment, msg.Imported)
		return m, nil

	case PostmanEnvironmentImportSaveFailedMsg:
		if msg.Err == nil {
			m.err = fmt.Errorf("postman environment import failed")
		} else {
			m.err = msg.Err
		}
		return m, nil

	case GRPCOpenMsg:
		m.openGRPCModal(msg.Edit)
		if msg.Discover && m.grpcModal != nil {
			m.grpcModal.discovering = true
			m.grpcModal.err = ""
			m.grpcDiscoverySeq++
			return m, m.discoverGRPC(m.grpcDiscoverySeq)
		}
		return m, nil

	case GRPCDiscoveredMsg:
		if msg.Token != 0 && msg.Token != m.grpcDiscoverySeq {
			return m, nil
		}
		if m.grpcModal == nil {
			if msg.Err != nil {
				m.err = msg.Err
			}
			return m, nil
		}
		m.grpcModal.discovering = false
		if msg.Err != nil {
			m.grpcModal.services = nil
			m.grpcModal.err = msg.Err.Error()
			return m, nil
		}
		m.grpcModal.err = ""
		m.grpcModal.services = msg.Services
		m.grpcModal.serviceIndex = 0
		m.grpcModal.methodIndex = 0
		if len(msg.Services) > 0 {
			m.grpcModal.selectService()
			m.statusMsg = fmt.Sprintf("discovered %d gRPC service(s) from %s", len(msg.Services), m.grpcModal.descriptorSource.String())
		} else {
			m.grpcModal.err = fmt.Sprintf("%s returned no services", m.grpcModal.descriptorSource.String())
		}
		return m, nil
	}

	// 命令面板模式下，所有键先交给 palette
	if m.focus == FocusCommand {
		return m.updatePalette(msg)
	}

	// Forms are fully modal.  Route every key to the active field before the
	// global bindings so a typed `q`, `s`, or `d` can never quit, send, or
	// delete a request by accident.
	if m.modal != nil {
		kmsg, ok := msg.(tea.KeyMsg)
		if !ok {
			return m, nil
		}
		return m.updateModal(kmsg)
	}
	if m.workspaceModal != nil {
		kmsg, ok := msg.(tea.KeyMsg)
		if !ok {
			return m, nil
		}
		return m.updateWorkspaceModal(kmsg)
	}
	if m.curlImport != nil {
		kmsg, ok := msg.(tea.KeyMsg)
		if !ok {
			return m, nil
		}
		return m.updateCurlImport(kmsg)
	}
	if m.grpcModal != nil {
		kmsg, ok := msg.(tea.KeyMsg)
		if !ok {
			return m, nil
		}
		return m.updateGRPCModal(kmsg)
	}

	// 搜索和历史都是模态交互。它们必须在全局快捷键之前处理，
	// 否则搜索文本里的 q/s 或历史浏览里的 q 会被误认为退出/发送。
	if m.searching {
		kmsg, ok := msg.(tea.KeyMsg)
		if !ok {
			return m, nil
		}
		return m.updateSearch(kmsg)
	}
	if m.showHistory {
		kmsg, ok := msg.(tea.KeyMsg)
		if !ok {
			return m, nil
		}
		return m.updateHistory(kmsg)
	}

	// 帮助页也是一个轻量模态层；q/Esc 关闭它，避免 q 直接退出程序。
	if m.showHelp {
		kmsg, ok := msg.(tea.KeyMsg)
		if !ok {
			return m, nil
		}
		if key.Matches(kmsg, keys.Help) || key.Matches(kmsg, keys.Quit) || key.Matches(kmsg, keys.Back) {
			m.showHelp = false
		}
		return m, nil
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
		if m.focus == FocusList {
			return m, tea.Quit
		}
		m.goBack()
		return m, nil
	case key.Matches(kmsg, keys.Back):
		m.goBack()
		return m, nil
	case key.Matches(kmsg, keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(kmsg, keys.Command):
		m.openPalette()
		return m, nil
	case key.Matches(kmsg, keys.Send):
		return m, m.sendCurrent()
	case key.Matches(kmsg, keys.Save):
		return m, m.saveCurrent()
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

// openPalette enters the command palette while remembering the active panel.
// Commands are transient, so closing the palette should return to the panel
// from which it was opened (request/response/list), not always to the list.
func (m *Model) openPalette() {
	m.returnFocus = m.focus
	m.focus = FocusCommand
	m.palette.Reset()
	m.palette.Focus()
}

// closePalette leaves the command palette and restores the previous panel.
func (m *Model) closePalette() {
	if m.returnFocus == FocusCommand {
		m.returnFocus = FocusList
	}
	m.focus = m.returnFocus
	m.palette.Blur()
}

// goBack implements the design's q/Back navigation. q quits only at the root
// list; from the request and response panels it moves one level up.
func (m *Model) goBack() {
	switch m.focus {
	case FocusResponse:
		m.focus = FocusRequest
	case FocusRequest:
		m.focus = FocusList
	}
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
		m.openNewRequestModal()
		return m, nil
	case key.Matches(msg, keys.Delete):
		m.openDeleteModal()
		return m, nil
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
			switch m.tab {
			case TabParams:
				if m.curReq.IsGRPC() {
					m.openGRPCModal(true)
					return m, nil
				}
				m.modal = newKVModal(TabParams, m.curReq.Params)
				m.modal.resize(m.width)
			case TabHeaders:
				values := m.curReq.Headers
				if m.curReq.IsGRPC() {
					values = grpcMetadata(m.curReq)
				}
				m.modal = newKVModal(TabHeaders, values)
				m.modal.resize(m.width)
			case TabBody:
				return m, editor.Open(m.curReq.Body)
			case TabAuth:
				if m.curReq.IsGRPC() {
					m.openGRPCModal(true)
					return m, nil
				}
				m.modal = newAuthModal(m.curReq)
				m.modal.resize(m.width)
			}
		}
		return m, nil
	case key.Matches(msg, keys.EditMeta):
		if m.curReq != nil {
			if m.curReq.IsGRPC() {
				m.openGRPCModal(true)
				return m, nil
			}
			m.modal = newMetaModal(m.curReq)
			m.modal.resize(m.width)
		}
		return m, nil
	case key.Matches(msg, keys.Delete):
		m.openDeleteModal()
		return m, nil
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
		if len(m.historyEntries) == 0 {
			m.showHistory = false
			m.statusMsg = "no history yet"
			return m, nil
		}
		if m.historyIdx < 0 {
			m.historyIdx = 0
		}
		if m.historyIdx >= len(m.historyEntries) {
			m.historyIdx = len(m.historyEntries) - 1
		}
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
		m.dirty = false
		m.statusMsg = "loaded from history — s to resend; history snapshot cannot be saved directly"
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
	if m.curReq.IsGRPC() {
		m.resp = nil
	} else {
		m.grpcResp = nil
	}
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
	m.viewport.Height = respH - 4 // 边框 2 + 状态行 1 + 分隔线 1
	if m.modal != nil {
		m.modal.resize(m.width)
	}
	if m.workspaceModal != nil {
		m.workspaceModal.resize(m.width)
	}
	if m.grpcModal != nil {
		m.grpcModal.resize(m.width)
	}
	if m.resp != nil {
		m.setResponseViewportContent(false)
	}
	if m.grpcResp != nil {
		m.setResponseViewportContent(false)
	}
}

// setResponseViewportContent updates the response pager after a response or a
// resize. The engine caps response reads, but the UI still needs to make that
// boundary explicit so a user never mistakes an incomplete body for a complete
// one. Keeping the marker in the viewport also makes it visible after scrolling
// to the bottom.
func (m *Model) setResponseViewportContent(reset bool) {
	if m.grpcResp != nil {
		content := HighlightJSON(m.grpcResp.Body)
		m.viewport.SetContent(content)
		if reset {
			m.viewport.GotoTop()
		}
		return
	}
	if m.resp == nil {
		m.viewport.SetContent("")
		return
	}
	content := HighlightJSON(m.resp.Body)
	if m.resp.Truncated {
		content = strings.TrimRight(content, "\n") + "\n\n" + truncatedStyle.Render("… response truncated by the read limit")
	}
	m.viewport.SetContent(content)
	if reset {
		m.viewport.GotoTop()
	}
}

// updateSearch 处理搜索模式的按键。
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.searching = false
		m.searchInput = textinput.Model{}
		m.list.SetItems(m.allItems())
		m.list.ResetSelected()
		m.selectCurrent()
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
		m.list.ResetSelected()
		m.selectCurrent()
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
	m.list.ResetSelected()
	m.selectCurrent()
	return m, cmd
}
