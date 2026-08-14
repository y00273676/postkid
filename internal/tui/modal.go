package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// modalKind identifies the small, purpose-built forms used by the TUI.  They
// deliberately stay separate from the application layer: a user can Esc out
// of any form without mutating the request or writing a collection.
type modalKind uint8

const (
	modalKV modalKind = iota + 1
	modalAuth
	modalMeta
	modalNew
	modalDelete
)

type kvEditRow struct {
	key   textinput.Model
	value textinput.Model
}

// modalState is intentionally a single value so Bubble Tea updates remain
// easy to reason about.  Only the fields needed by the active kind are used.
type modalState struct {
	kind  modalKind
	title string
	err   string

	// key/value editor
	rows  []kvEditRow
	row   int
	col   int // 0 = key, 1 = value
	kvTab tab

	// auth editor: 0 = type, 1 = username, 2 = password, 3 = token.
	authType  string
	authFocus int
	authUser  textinput.Model
	authPass  textinput.Model
	authToken textinput.Model

	// request metadata editor
	metaMethod textinput.Model
	metaURL    textinput.Model
	metaFocus  int // 0 = method, 1 = URL

	// new request editor: 0 = collection, 1 = name, 2 = method, 3 = URL.
	newColl   int
	newFocus  int
	newName   textinput.Model
	newMethod textinput.Model
	newURL    textinput.Model

	// delete confirmation
	deleteColl *model.Collection
	deleteName string
}

func newInput(value, placeholder string) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.PromptStyle = valueStyle
	in.TextStyle = valueStyle
	in.Placeholder = placeholder
	in.CharLimit = 0
	in.Width = 42
	in.SetValue(value)
	return in
}

func newSecretInput(value, placeholder string) textinput.Model {
	in := newInput(value, placeholder)
	in.EchoMode = textinput.EchoPassword
	in.EchoCharacter = '•'
	return in
}

func sortedKVRows(values map[string]string) []kvEditRow {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]kvEditRow, 0, len(keys)+1)
	for _, k := range keys {
		rows = append(rows, kvEditRow{key: newInput(k, "key"), value: newInput(values[k], "value")})
	}
	// Always leave an empty row available. This makes adding a first value as
	// fast as editing an existing map, while empty rows are ignored on save.
	if len(rows) == 0 || rows[len(rows)-1].key.Value() != "" || rows[len(rows)-1].value.Value() != "" {
		rows = append(rows, kvEditRow{key: newInput("", "key"), value: newInput("", "value")})
	}
	return rows
}

func newKVModal(which tab, values map[string]string) *modalState {
	m := &modalState{
		kind:  modalKV,
		title: map[tab]string{TabParams: "Edit Params", TabHeaders: "Edit Headers"}[which],
		rows:  sortedKVRows(values),
		kvTab: which,
	}
	m.focusKVInput()
	return m
}

func newAuthModal(req *model.Request) *modalState {
	typeName := strings.ToLower(strings.TrimSpace(req.AuthType))
	if typeName == "" {
		typeName = model.AuthNone
	}
	m := &modalState{
		kind:      modalAuth,
		title:     "Edit Auth",
		authType:  typeName,
		authUser:  newInput(req.AuthUsername, "username"),
		authPass:  newSecretInput(req.AuthPassword, "password"),
		authToken: newSecretInput(req.AuthToken, "token"),
	}
	m.focusAuthInput()
	return m
}

func newMetaModal(req *model.Request) *modalState {
	m := &modalState{
		kind:       modalMeta,
		title:      "Edit Request",
		metaMethod: newInput(strings.ToUpper(req.Method), "method (GET/POST/PUT/PATCH/DELETE)"),
		metaURL:    newInput(req.URL, "https://example.com/path"),
	}
	m.focusMetaInput()
	return m
}

func newNewRequestModal(colls []*model.Collection, selected *model.Collection) *modalState {
	idx := 0
	if selected != nil {
		for i, c := range colls {
			if c != nil && c.FilePath == selected.FilePath && c.Name == selected.Name {
				idx = i
				break
			}
		}
	}
	m := &modalState{
		kind:      modalNew,
		title:     "New Request",
		newColl:   idx,
		newFocus:  0,
		newName:   newInput("", "request-name"),
		newMethod: newInput("GET", "method"),
		newURL:    newInput("https://", "https://example.com/path"),
	}
	m.focusNewInput()
	return m
}

func newDeleteModal(coll *model.Collection, name string) *modalState {
	return &modalState{kind: modalDelete, title: "Delete Request", deleteColl: coll, deleteName: name}
}

func (m *modalState) inputWidth(screenWidth int) int {
	width := screenWidth - 24
	if width < 18 {
		width = 18
	}
	if width > 64 {
		width = 64
	}
	return width
}

func (m *modalState) resize(screenWidth int) {
	if m == nil {
		return
	}
	w := m.inputWidth(screenWidth)
	for i := range m.rows {
		m.rows[i].key.Width = w / 3
		if m.rows[i].key.Width < 10 {
			m.rows[i].key.Width = 10
		}
		m.rows[i].value.Width = w - m.rows[i].key.Width - 3
		if m.rows[i].value.Width < 10 {
			m.rows[i].value.Width = 10
		}
	}
	for _, in := range []*textinput.Model{&m.authUser, &m.authPass, &m.authToken, &m.metaMethod, &m.metaURL, &m.newName, &m.newMethod, &m.newURL} {
		in.Width = w
	}
}

func (m *modalState) focusInput(in *textinput.Model) {
	if in == nil {
		return
	}
	in.Focus()
}

func (m *modalState) blurInput(in *textinput.Model) {
	if in == nil {
		return
	}
	in.Blur()
}

func (m *modalState) focusKVInput() {
	if len(m.rows) == 0 {
		return
	}
	if m.row < 0 {
		m.row = 0
	}
	if m.row >= len(m.rows) {
		m.row = len(m.rows) - 1
	}
	for i := range m.rows {
		m.rows[i].key.Blur()
		m.rows[i].value.Blur()
	}
	if m.col == 0 {
		m.rows[m.row].key.Focus()
	} else {
		m.rows[m.row].value.Focus()
	}
}

func (m *modalState) focusAuthInput() {
	m.authUser.Blur()
	m.authPass.Blur()
	m.authToken.Blur()
	switch m.authFocus {
	case 1:
		m.authUser.Focus()
	case 2:
		m.authPass.Focus()
	case 3:
		m.authToken.Focus()
	}
}

func (m *modalState) focusMetaInput() {
	m.metaMethod.Blur()
	m.metaURL.Blur()
	if m.metaFocus == 0 {
		m.metaMethod.Focus()
	} else {
		m.metaURL.Focus()
	}
}

func (m *modalState) focusNewInput() {
	m.newName.Blur()
	m.newMethod.Blur()
	m.newURL.Blur()
	switch m.newFocus {
	case 1:
		m.newName.Focus()
	case 2:
		m.newMethod.Focus()
	case 3:
		m.newURL.Focus()
	}
}

func (m *modalState) setError(err error) {
	if err == nil {
		m.err = ""
		return
	}
	m.err = err.Error()
}

func (m *modalState) updateKV(msg tea.KeyMsg) tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	if isTab(msg) {
		if isReverseTab(msg) {
			m.col--
			if m.col < 0 {
				m.col = 1
				m.row--
			}
		} else {
			m.col++
			if m.col > 1 {
				m.col = 0
				m.row++
			}
		}
		if m.row < 0 {
			m.row = len(m.rows) - 1
		}
		if m.row >= len(m.rows) {
			m.row = 0
		}
		m.focusKVInput()
		return nil
	}
	if msg.Type == tea.KeyUp || msg.Type == tea.KeyDown {
		if msg.Type == tea.KeyUp {
			m.row--
		} else {
			m.row++
		}
		if m.row < 0 {
			m.row = len(m.rows) - 1
		}
		if m.row >= len(m.rows) {
			m.row = 0
		}
		m.focusKVInput()
		return nil
	}
	if msg.Type == tea.KeyCtrlN {
		m.rows = append(m.rows, kvEditRow{key: newInput("", "key"), value: newInput("", "value")})
		m.row = len(m.rows) - 1
		m.col = 0
		m.focusKVInput()
		return nil
	}
	if msg.Type == tea.KeyCtrlD {
		if len(m.rows) > 1 {
			m.rows = append(m.rows[:m.row], m.rows[m.row+1:]...)
			if m.row >= len(m.rows) {
				m.row = len(m.rows) - 1
			}
		} else {
			m.rows[0] = kvEditRow{key: newInput("", "key"), value: newInput("", "value")}
		}
		m.focusKVInput()
		return nil
	}
	var cmd tea.Cmd
	if m.col == 0 {
		m.rows[m.row].key, cmd = m.rows[m.row].key.Update(msg)
	} else {
		m.rows[m.row].value, cmd = m.rows[m.row].value.Update(msg)
	}
	return cmd
}

func isTab(msg tea.KeyMsg) bool { return msg.Type == tea.KeyTab || msg.Type == tea.KeyShiftTab }

func isReverseTab(msg tea.KeyMsg) bool { return msg.Type == tea.KeyShiftTab }

func (m *modalState) updateAuth(msg tea.KeyMsg) tea.Cmd {
	if isTab(msg) {
		if isReverseTab(msg) {
			m.authFocus--
			if m.authFocus < 0 {
				m.authFocus = 3
			}
		} else {
			m.authFocus = (m.authFocus + 1) % 4
		}
		m.focusAuthInput()
		return nil
	}
	if m.authFocus == 0 {
		switch strings.ToLower(string(msg.Runes)) {
		case "n":
			m.authType = model.AuthNone
		case "b":
			m.authType = model.AuthBasic
		case "r":
			m.authType = model.AuthBearer
		}
		if msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight {
			choices := []string{model.AuthNone, model.AuthBasic, model.AuthBearer}
			idx := 0
			for i, choice := range choices {
				if choice == m.authType {
					idx = i
				}
			}
			if msg.Type == tea.KeyLeft {
				idx = (idx + len(choices) - 1) % len(choices)
			} else {
				idx = (idx + 1) % len(choices)
			}
			m.authType = choices[idx]
		}
		return nil
	}
	var cmd tea.Cmd
	switch m.authFocus {
	case 1:
		m.authUser, cmd = m.authUser.Update(msg)
	case 2:
		m.authPass, cmd = m.authPass.Update(msg)
	case 3:
		m.authToken, cmd = m.authToken.Update(msg)
	}
	return cmd
}

func (m *modalState) updateMeta(msg tea.KeyMsg) tea.Cmd {
	if isTab(msg) {
		if isReverseTab(msg) {
			m.metaFocus = (m.metaFocus + 1) % 2
		} else {
			m.metaFocus = (m.metaFocus + 1) % 2
		}
		m.focusMetaInput()
		return nil
	}
	var cmd tea.Cmd
	if m.metaFocus == 0 {
		m.metaMethod, cmd = m.metaMethod.Update(msg)
	} else {
		m.metaURL, cmd = m.metaURL.Update(msg)
	}
	return cmd
}

func (m *modalState) updateNew(msg tea.KeyMsg, collCount int) tea.Cmd {
	if m.newFocus == 0 {
		if msg.Type == tea.KeyUp || msg.Type == tea.KeyLeft {
			if collCount > 0 {
				m.newColl = (m.newColl + collCount - 1) % collCount
			}
			return nil
		}
		if msg.Type == tea.KeyDown || msg.Type == tea.KeyRight {
			if collCount > 0 {
				m.newColl = (m.newColl + 1) % collCount
			}
			return nil
		}
	}
	if isTab(msg) {
		if isReverseTab(msg) {
			m.newFocus--
			if m.newFocus < 0 {
				m.newFocus = 3
			}
		} else {
			m.newFocus = (m.newFocus + 1) % 4
		}
		m.focusNewInput()
		return nil
	}
	var cmd tea.Cmd
	switch m.newFocus {
	case 1:
		m.newName, cmd = m.newName.Update(msg)
	case 2:
		m.newMethod, cmd = m.newMethod.Update(msg)
	case 3:
		m.newURL, cmd = m.newURL.Update(msg)
	}
	return cmd
}

// modalContent renders a compact form that works at normal terminal sizes and
// remains readable when the terminal is resized down to a narrow window.
func (m Model) modalContent() string {
	if m.modal == nil {
		return ""
	}
	state := m.modal
	lines := []string{titleStyle.Render(state.title), ""}
	switch state.kind {
	case modalKV:
		lines = append(lines, mutedStyle.Render("Tab next field · Ctrl+N add · Ctrl+D remove · Enter save · Esc cancel"), "")
		for i, row := range state.rows {
			marker := "  "
			if i == state.row {
				marker = lipgloss.NewStyle().Foreground(colAccent).Render("▎ ")
			}
			lines = append(lines, marker+row.key.View()+punctStyle.Render(" : ")+row.value.View())
		}
	case modalAuth:
		lines = append(lines, mutedStyle.Render("←/→ or n/b/r choose type · Tab fields · Enter save · Esc cancel"), "")
		lines = append(lines, renderAuthChoices(state.authType))
		lines = append(lines, "")
		lines = append(lines,
			modalField("Username", state.authUser.View(), state.authFocus == 1),
			modalField("Password", state.authPass.View(), state.authFocus == 2),
			modalField("Token", state.authToken.View(), state.authFocus == 3),
		)
	case modalMeta:
		lines = append(lines, mutedStyle.Render("Tab next field · Enter save · Esc cancel"), "")
		lines = append(lines,
			modalField("Method", state.metaMethod.View(), state.metaFocus == 0),
			modalField("URL", state.metaURL.View(), state.metaFocus == 1),
		)
	case modalNew:
		lines = append(lines, mutedStyle.Render("↑/↓ choose collection · Tab fields · Enter create · Esc cancel"), "")
		collName := "(no collections)"
		if state.newColl >= 0 && state.newColl < len(m.colls) {
			collName = m.colls[state.newColl].Name
		}
		collectionLine := keyStyle.Render("Collection") + punctStyle.Render(" : ") + valueStyle.Render(collName)
		if state.newFocus == 0 {
			collectionLine = lipgloss.NewStyle().Foreground(colAccent).Render("▎ ") + collectionLine
		} else {
			collectionLine = "  " + collectionLine
		}
		lines = append(lines, collectionLine,
			modalField("Name", state.newName.View(), state.newFocus == 1),
			modalField("Method", state.newMethod.View(), state.newFocus == 2),
			modalField("URL", state.newURL.View(), state.newFocus == 3),
		)
	case modalDelete:
		lines = append(lines,
			errStyle.Render(fmt.Sprintf("Delete %q from %q?", state.deleteName, collectionName(state.deleteColl))),
			"",
			mutedStyle.Render("Enter confirm · Esc cancel"),
		)
	}
	if state.err != "" {
		lines = append(lines, "", errStyle.Render(state.err))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func collectionName(coll *model.Collection) string {
	if coll == nil {
		return "?"
	}
	return coll.Name
}

func modalField(label, value string, focused bool) string {
	marker := "  "
	if focused {
		marker = lipgloss.NewStyle().Foreground(colAccent).Render("▎ ")
	}
	return marker + keyStyle.Render(label) + punctStyle.Render(" : ") + value
}

func renderAuthChoices(selected string) string {
	choices := []string{model.AuthNone, model.AuthBasic, model.AuthBearer}
	parts := make([]string, 0, len(choices))
	for _, choice := range choices {
		label := " " + choice + " "
		if choice == selected {
			parts = append(parts, tabActive.Render("["+label+"]"))
		} else {
			parts = append(parts, tabInactive.Render(" "+label+" "))
		}
	}
	return strings.Join(parts, " ")
}

// updateModal owns all keyboard handling while a form is open.  The caller
// has already intercepted global bindings, so these handlers can safely use
// Enter/Esc/Ctrl+S without leaking them to the surrounding panel.
func (m Model) updateModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modal == nil {
		return m, nil
	}
	if msg.Type == tea.KeyEscape {
		m.modal = nil
		return m, nil
	}
	if msg.Type == tea.KeyEnter || msg.Type == tea.KeyCtrlS {
		switch m.modal.kind {
		case modalKV:
			if err := m.commitKVModal(); err != nil {
				m.modal.setError(err)
				return m, nil
			}
		case modalAuth:
			if err := m.commitAuthModal(); err != nil {
				m.modal.setError(err)
				return m, nil
			}
		case modalMeta:
			if err := m.commitMetaModal(); err != nil {
				m.modal.setError(err)
				return m, nil
			}
		case modalNew:
			cmd, err := m.commitNewModal()
			if err != nil {
				m.modal.setError(err)
				return m, nil
			}
			m.modal = nil
			return m, cmd
		case modalDelete:
			cmd := m.confirmDeleteCmd()
			m.modal = nil
			return m, cmd
		}
		m.modal = nil
		return m, nil
	}

	var cmd tea.Cmd
	switch m.modal.kind {
	case modalKV:
		cmd = m.modal.updateKV(msg)
	case modalAuth:
		cmd = m.modal.updateAuth(msg)
	case modalMeta:
		cmd = m.modal.updateMeta(msg)
	case modalNew:
		cmd = m.modal.updateNew(msg, len(m.colls))
	case modalDelete:
		// Delete confirmation intentionally accepts only Enter/Esc. Any other
		// key is ignored, preventing an accidental global action.
		return m, nil
	}
	return m, cmd
}

func (m *Model) commitKVModal() error {
	if m.curReq == nil || m.modal == nil {
		return fmt.Errorf("no request selected")
	}
	values := make(map[string]string)
	headerKeys := make(map[string]string)
	for _, row := range m.modal.rows {
		key := strings.TrimSpace(row.key.Value())
		value := row.value.Value()
		if key == "" && strings.TrimSpace(value) == "" {
			continue
		}
		if key == "" {
			return fmt.Errorf("key cannot be empty")
		}
		duplicateKey := key
		if m.modal.kvTab == TabHeaders {
			folded := strings.ToLower(key)
			if previous, exists := headerKeys[folded]; exists {
				return fmt.Errorf("duplicate header %q conflicts with %q", key, previous)
			}
			headerKeys[folded] = key
		}
		if _, exists := values[duplicateKey]; exists {
			return fmt.Errorf("duplicate key %q", key)
		}
		values[key] = value
	}
	if m.modal.kvTab == TabParams {
		m.curReq.Params = values
	} else {
		if m.curReq.IsGRPC() {
			if m.curReq.GRPC == nil {
				m.curReq.GRPC = &model.GRPCRequest{}
			}
			m.curReq.GRPC.Metadata = values
			m.curReq.Headers = nil
		} else {
			m.curReq.Headers = values
		}
	}
	m.dirty = true
	m.statusMsg = "edited — Ctrl+S to save"
	return nil
}

func (m *Model) commitAuthModal() error {
	if m.curReq == nil || m.modal == nil {
		return fmt.Errorf("no request selected")
	}
	typeName := strings.ToLower(strings.TrimSpace(m.modal.authType))
	switch typeName {
	case model.AuthNone, model.AuthBasic, model.AuthBearer:
	default:
		return fmt.Errorf("unsupported auth type %q", m.modal.authType)
	}
	m.curReq.AuthType = typeName
	m.curReq.AuthUsername = m.modal.authUser.Value()
	m.curReq.AuthPassword = m.modal.authPass.Value()
	m.curReq.AuthToken = m.modal.authToken.Value()
	m.dirty = true
	m.statusMsg = "auth edited — Ctrl+S to save"
	return nil
}

func (m *Model) commitMetaModal() error {
	if m.curReq == nil || m.modal == nil {
		return fmt.Errorf("no request selected")
	}
	method := strings.ToUpper(strings.TrimSpace(m.modal.metaMethod.Value()))
	if !model.IsValidMethod(method) {
		return fmt.Errorf("unsupported HTTP method %q", method)
	}
	rawURL := strings.TrimSpace(m.modal.metaURL.Value())
	if rawURL == "" {
		return fmt.Errorf("URL cannot be empty")
	}
	m.curReq.Method = method
	m.curReq.URL = rawURL
	m.dirty = true
	m.statusMsg = "request edited — Ctrl+S to save"
	return nil
}

func (m *Model) commitNewModal() (tea.Cmd, error) {
	if m.modal == nil || m.modal.kind != modalNew {
		return nil, fmt.Errorf("new request form is not open")
	}
	if len(m.colls) == 0 {
		return nil, fmt.Errorf("no collection available")
	}
	if m.modal.newColl < 0 || m.modal.newColl >= len(m.colls) {
		return nil, fmt.Errorf("invalid collection selection")
	}
	name := strings.TrimSpace(m.modal.newName.Value())
	if name == "" {
		return nil, fmt.Errorf("request name cannot be empty")
	}
	method := strings.ToUpper(strings.TrimSpace(m.modal.newMethod.Value()))
	if !model.IsValidMethod(method) {
		return nil, fmt.Errorf("unsupported HTTP method %q", method)
	}
	rawURL := strings.TrimSpace(m.modal.newURL.Value())
	if rawURL == "" {
		return nil, fmt.Errorf("URL cannot be empty")
	}
	req := model.Request{Name: name, Method: method, URL: rawURL}
	return m.addNewRequestCmd(m.colls[m.modal.newColl], req), nil
}

func (m *Model) openNewRequestModal() {
	if len(m.colls) == 0 {
		m.err = fmt.Errorf("no collection available")
		return
	}
	m.modal = newNewRequestModal(m.colls, m.curColl)
	m.modal.resize(m.width)
	m.err = nil
}

func (m *Model) openDeleteModal() {
	if m.curReq == nil || m.curColl == nil {
		m.err = fmt.Errorf("no request selected")
		return
	}
	m.modal = newDeleteModal(m.curColl, m.curReq.Name)
	m.modal.resize(m.width)
	m.err = nil
}

func (m Model) addNewRequestCmd(coll *model.Collection, req model.Request) tea.Cmd {
	if coll == nil {
		return m.errorCmd(fmt.Errorf("no collection selected"))
	}
	app := m.app
	return func() tea.Msg {
		if err := app.AddRequest(coll, &req); err != nil {
			return ErrorMsg{Err: err}
		}
		return ListUpdatedMsg{}
	}
}

func (m Model) confirmDeleteCmd() tea.Cmd {
	if m.modal == nil || m.modal.deleteColl == nil || m.modal.deleteName == "" {
		return m.errorCmd(fmt.Errorf("no request selected"))
	}
	coll := m.modal.deleteColl
	name := m.modal.deleteName
	app := m.app
	return func() tea.Msg {
		if err := app.DeleteRequest(coll, name); err != nil {
			return ErrorMsg{Err: err}
		}
		return ListUpdatedMsg{}
	}
}
