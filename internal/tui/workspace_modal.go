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

type workspaceKind uint8

const (
	workspaceCollectionNew workspaceKind = iota + 1
	workspaceCollectionRename
	workspaceCollectionDelete
	workspaceEnvironmentNew
	workspaceEnvironmentRename
	workspaceEnvironmentDelete
)

const (
	workspaceFocusTarget = iota
	workspaceFocusName
	workspaceFocusVariables
)

type workspaceKVRow struct {
	key   textinput.Model
	value textinput.Model
}

// workspaceModalState owns collection/environment forms independently of the
// request editor in modal.go.  In particular, targets come from the complete
// application snapshots, so an empty collection is still selectable even
// though the left request list has no row for it.
type workspaceModalState struct {
	kind  workspaceKind
	title string
	err   string

	targets  []string
	selected int
	focus    int
	name     textinput.Model

	rows       []workspaceKVRow
	row        int
	col        int
	originalKV map[string]string
}

func workspaceInput(value, placeholder string) textinput.Model {
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

func workspaceKVRows(values map[string]string) []workspaceKVRow {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]workspaceKVRow, 0, len(keys)+1)
	for _, key := range keys {
		rows = append(rows, workspaceKVRow{
			key:   workspaceInput(key, "key"),
			value: workspaceInput(values[key], "value"),
		})
	}
	// Keep one blank row available so an empty environment can immediately
	// receive its first variable.
	rows = append(rows, workspaceKVRow{
		key:   workspaceInput("", "key"),
		value: workspaceInput("", "value"),
	})
	return rows
}

func workspaceCopyMap(values map[string]string) map[string]string {
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func workspaceNames(resource string, m Model) []string {
	var names []string
	if resource == "environment" {
		for _, env := range m.app.Environments() {
			names = append(names, env.Name)
		}
		return names
	}
	for _, coll := range m.app.Collections() {
		names = append(names, coll.Name)
	}
	return names
}

func workspaceTargetIndex(target string, names []string) int {
	if target != "" {
		for i, name := range names {
			if name == target {
				return i
			}
		}
	}
	return 0
}

func workspaceKindFor(resource, action string) (workspaceKind, error) {
	resource = strings.ToLower(strings.TrimSpace(resource))
	action = strings.ToLower(strings.TrimSpace(action))
	switch resource + ":" + action {
	case "collection:new":
		return workspaceCollectionNew, nil
	case "collection:rename":
		return workspaceCollectionRename, nil
	case "collection:delete":
		return workspaceCollectionDelete, nil
	case "environment:new":
		return workspaceEnvironmentNew, nil
	case "environment:rename":
		return workspaceEnvironmentRename, nil
	case "environment:delete":
		return workspaceEnvironmentDelete, nil
	default:
		return 0, fmt.Errorf("unknown workspace action %q %q", resource, action)
	}
}

func (m Model) newWorkspaceModal(resource, action, target string) (*workspaceModalState, error) {
	kind, err := workspaceKindFor(resource, action)
	if err != nil {
		return nil, err
	}
	if m.app == nil {
		return nil, fmt.Errorf("application unavailable")
	}
	names := workspaceNames(resource, m)
	needsTarget := kind == workspaceCollectionRename || kind == workspaceCollectionDelete ||
		kind == workspaceEnvironmentRename || kind == workspaceEnvironmentDelete
	if needsTarget && len(names) == 0 {
		return nil, fmt.Errorf("no %s available", resource)
	}
	if needsTarget && target != "" && workspaceTargetIndex(target, names) == 0 && names[0] != target {
		return nil, fmt.Errorf("%s %q not found", resource, target)
	}

	state := &workspaceModalState{
		kind:     kind,
		targets:  names,
		selected: workspaceTargetIndex(target, names),
		focus:    workspaceFocusName,
		name:     workspaceInput("", "name"),
	}
	switch kind {
	case workspaceCollectionNew:
		state.title = "New Collection"
	case workspaceCollectionRename:
		state.title = "Rename Collection"
		state.name.SetValue(names[state.selected])
		state.focus = workspaceFocusTarget
	case workspaceCollectionDelete:
		state.title = "Delete Collection"
		state.focus = workspaceFocusTarget
	case workspaceEnvironmentNew:
		state.title = "New Environment"
		state.rows = workspaceKVRows(nil)
	case workspaceEnvironmentRename:
		state.title = "Edit Environment"
		state.name.SetValue(names[state.selected])
		state.rows = workspaceKVRows(m.app.Environments()[state.selected].Variables)
		state.originalKV = workspaceCopyMap(m.app.Environments()[state.selected].Variables)
		state.focus = workspaceFocusTarget
	case workspaceEnvironmentDelete:
		state.title = "Delete Environment"
		state.focus = workspaceFocusTarget
	}
	state.focusActiveInput()
	state.resize(m.width)
	return state, nil
}

func (m *workspaceModalState) resize(screenWidth int) {
	if m == nil {
		return
	}
	width := screenWidth - 24
	if width < 18 {
		width = 18
	}
	if width > 64 {
		width = 64
	}
	m.name.Width = width
	for i := range m.rows {
		m.rows[i].key.Width = max(10, width/3)
		m.rows[i].value.Width = max(10, width-m.rows[i].key.Width-3)
	}
}

func (m *workspaceModalState) hasTarget() bool {
	return m.kind == workspaceCollectionRename || m.kind == workspaceCollectionDelete ||
		m.kind == workspaceEnvironmentRename || m.kind == workspaceEnvironmentDelete
}

func (m *workspaceModalState) hasName() bool {
	return m.kind == workspaceCollectionNew || m.kind == workspaceCollectionRename ||
		m.kind == workspaceEnvironmentNew || m.kind == workspaceEnvironmentRename
}

func (m *workspaceModalState) hasVariables() bool {
	return m.kind == workspaceEnvironmentNew || m.kind == workspaceEnvironmentRename
}

func (m *workspaceModalState) firstFocus() int {
	if m.hasTarget() {
		return workspaceFocusTarget
	}
	return workspaceFocusName
}

func (m *workspaceModalState) lastFocus() int {
	if m.hasVariables() {
		return workspaceFocusVariables
	}
	return workspaceFocusName
}

func (m *workspaceModalState) focusActiveInput() {
	if m == nil {
		return
	}
	m.name.Blur()
	for i := range m.rows {
		m.rows[i].key.Blur()
		m.rows[i].value.Blur()
	}
	if m.focus == workspaceFocusName && m.hasName() {
		m.name.Focus()
	}
	if m.focus == workspaceFocusVariables && m.hasVariables() && len(m.rows) > 0 {
		m.row = min(max(m.row, 0), len(m.rows)-1)
		if m.col == 0 {
			m.rows[m.row].key.Focus()
		} else {
			m.rows[m.row].value.Focus()
		}
	}
}

func (m *workspaceModalState) moveTarget(delta int) {
	if len(m.targets) == 0 {
		return
	}
	m.selected = (m.selected + delta + len(m.targets)) % len(m.targets)
	if m.kind == workspaceCollectionRename || m.kind == workspaceEnvironmentRename {
		m.name.SetValue(m.targets[m.selected])
	}
}

func (m *workspaceModalState) syncSelectedEnvironment(appModel Model) {
	if m == nil || m.kind != workspaceEnvironmentRename || appModel.app == nil || len(m.targets) == 0 {
		return
	}
	environments := appModel.app.Environments()
	if m.selected < 0 || m.selected >= len(environments) {
		return
	}
	m.name.SetValue(m.targets[m.selected])
	m.rows = workspaceKVRows(environments[m.selected].Variables)
	m.originalKV = workspaceCopyMap(environments[m.selected].Variables)
	m.row = 0
	m.col = 0
	m.focusActiveInput()
}

func workspaceKey(msg tea.KeyMsg, keyType tea.KeyType, runeValue rune) bool {
	if msg.Type == keyType {
		return true
	}
	return msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == runeValue
}

func (m *workspaceModalState) advanceTopField(reverse bool) {
	fields := []int{}
	if m.hasTarget() {
		fields = append(fields, workspaceFocusTarget)
	}
	if m.hasName() {
		fields = append(fields, workspaceFocusName)
	}
	if m.hasVariables() {
		fields = append(fields, workspaceFocusVariables)
	}
	if len(fields) == 0 {
		return
	}
	idx := 0
	for i, field := range fields {
		if field == m.focus {
			idx = i
			break
		}
	}
	if reverse {
		idx = (idx + len(fields) - 1) % len(fields)
	} else {
		idx = (idx + 1) % len(fields)
	}
	m.focus = fields[idx]
	m.focusActiveInput()
}

// updateWorkspaceVariables handles a two-column key/value grid. Tab walks
// key→value→next row and leaves the grid only after the final value.
func (m *workspaceModalState) updateWorkspaceVariables(msg tea.KeyMsg) tea.Cmd {
	if len(m.rows) == 0 {
		m.rows = workspaceKVRows(nil)
	}
	if isTab(msg) {
		if isReverseTab(msg) {
			if m.col == 1 {
				m.col = 0
			} else if m.row > 0 {
				m.row--
				m.col = 1
			} else {
				m.advanceTopField(true)
				return nil
			}
		} else if m.col == 0 {
			m.col = 1
		} else if m.row+1 < len(m.rows) {
			m.row++
			m.col = 0
		} else {
			m.advanceTopField(false)
			return nil
		}
		m.focusActiveInput()
		return nil
	}
	if msg.Type == tea.KeyCtrlN {
		m.rows = append(m.rows, workspaceKVRow{
			key:   workspaceInput("", "key"),
			value: workspaceInput("", "value"),
		})
		m.row = len(m.rows) - 1
		m.col = 0
		m.focusActiveInput()
		return nil
	}
	if msg.Type == tea.KeyCtrlD {
		if len(m.rows) > 1 {
			m.rows = append(m.rows[:m.row], m.rows[m.row+1:]...)
			if m.row >= len(m.rows) {
				m.row = len(m.rows) - 1
			}
		} else {
			m.rows[0] = workspaceKVRow{key: workspaceInput("", "key"), value: workspaceInput("", "value")}
		}
		m.focusActiveInput()
		return nil
	}
	if workspaceKey(msg, tea.KeyUp, 'k') {
		m.row = (m.row + len(m.rows) - 1) % len(m.rows)
		m.focusActiveInput()
		return nil
	}
	if workspaceKey(msg, tea.KeyDown, 'j') {
		m.row = (m.row + 1) % len(m.rows)
		m.focusActiveInput()
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

func (m *workspaceModalState) update(msg tea.KeyMsg) tea.Cmd {
	if m.kind == workspaceCollectionDelete || m.kind == workspaceEnvironmentDelete {
		// Only Enter/y confirms. Every other rune, including q, is inert while
		// the confirmation is open and cannot leak to the application shell.
		return nil
	}
	if isTab(msg) {
		if m.focus == workspaceFocusVariables {
			return m.updateWorkspaceVariables(msg)
		}
		m.advanceTopField(isReverseTab(msg))
		return nil
	}
	if m.focus == workspaceFocusTarget {
		if workspaceKey(msg, tea.KeyUp, 'k') || workspaceKey(msg, tea.KeyLeft, 'h') {
			m.moveTarget(-1)
			return nil
		}
		if workspaceKey(msg, tea.KeyDown, 'j') || workspaceKey(msg, tea.KeyRight, 'l') {
			m.moveTarget(1)
			return nil
		}
		return nil
	}
	if m.focus == workspaceFocusVariables {
		return m.updateWorkspaceVariables(msg)
	}
	var cmd tea.Cmd
	m.name, cmd = m.name.Update(msg)
	return cmd
}

func (m *workspaceModalState) selectedTarget() string {
	if m == nil || len(m.targets) == 0 || m.selected < 0 || m.selected >= len(m.targets) {
		return ""
	}
	return m.targets[m.selected]
}

func (m *workspaceModalState) variables() (map[string]string, error) {
	values := make(map[string]string)
	for _, row := range m.rows {
		key := strings.TrimSpace(row.key.Value())
		value := row.value.Value()
		if key == "" && strings.TrimSpace(value) == "" {
			continue
		}
		if key == "" {
			return nil, fmt.Errorf("variable key cannot be empty")
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("duplicate variable %q", key)
		}
		values[key] = value
	}
	return values, nil
}

func (m *workspaceModalState) workspaceName() (string, error) {
	name := strings.TrimSpace(m.name.Value())
	if name == "" {
		return "", fmt.Errorf("name cannot be empty")
	}
	if strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("name %q cannot contain path separators", name)
	}
	return name, nil
}

func (m *workspaceModalState) variablesChanged(values map[string]string) bool {
	if len(values) != len(m.originalKV) {
		return true
	}
	for key, value := range values {
		if m.originalKV[key] != value {
			return true
		}
	}
	return false
}

func (m Model) workspaceAvailable(resource string) error {
	if m.app == nil {
		return fmt.Errorf("application unavailable")
	}
	return nil
}

func workspaceCollectionTarget(a *Model, name string) (*model.Collection, error) {
	if a == nil || a.app == nil {
		return nil, fmt.Errorf("application unavailable")
	}
	collections := a.app.Collections()
	for i := range collections {
		if collections[i].Name == name {
			return &collections[i], nil
		}
	}
	return nil, fmt.Errorf("collection %q not found", name)
}

func (m *Model) openWorkspaceModal(msg WorkspaceModalMsg) {
	state, err := m.newWorkspaceModal(msg.Resource, msg.Action, msg.Target)
	if err != nil {
		m.err = err
		return
	}
	m.workspaceModal = state
	m.err = nil
}

func (m Model) workspaceUpdated(resource, action, name string) tea.Msg {
	return WorkspaceUpdatedMsg{Resource: resource, Action: action, Name: name}
}

func (m Model) workspaceCommand(resource, action, target, name string, variables map[string]string, variablesChanged bool) tea.Cmd {
	appModel := m
	return func() tea.Msg {
		var err error
		switch resource + ":" + action {
		case "collection:new":
			_, err = appModel.app.CreateCollection(name)
		case "collection:rename":
			var collection *model.Collection
			collection, err = workspaceCollectionTarget(&appModel, target)
			if err == nil {
				err = appModel.app.RenameCollection(collection, name)
			}
		case "collection:delete":
			var collection *model.Collection
			collection, err = workspaceCollectionTarget(&appModel, target)
			if err == nil {
				err = appModel.app.DeleteCollection(collection)
			}
		case "environment:new":
			err = appModel.app.CreateEnvironment(name, variables)
		case "environment:rename":
			_ = variablesChanged
			err = appModel.app.EditEnvironment(target, name, variables)
		case "environment:delete":
			err = appModel.app.DeleteEnvironment(target)
		default:
			return ErrorMsg{Err: fmt.Errorf("unknown workspace action %q %q", resource, action)}
		}
		if err != nil {
			return ErrorMsg{Err: err}
		}
		return appModel.workspaceUpdated(resource, action, name)
	}
}

func (m Model) commitWorkspaceModal() (tea.Cmd, error) {
	state := m.workspaceModal
	if state == nil {
		return nil, fmt.Errorf("workspace form is not open")
	}
	resource := "collection"
	if state.kind == workspaceEnvironmentNew || state.kind == workspaceEnvironmentRename || state.kind == workspaceEnvironmentDelete {
		resource = "environment"
	}
	action := "new"
	if state.kind == workspaceCollectionRename || state.kind == workspaceEnvironmentRename {
		action = "rename"
	}
	if state.kind == workspaceCollectionDelete || state.kind == workspaceEnvironmentDelete {
		action = "delete"
	}
	if err := m.workspaceAvailable(resource); err != nil {
		return nil, err
	}
	target := state.selectedTarget()
	if action == "delete" {
		if target == "" {
			return nil, fmt.Errorf("no %s selected", resource)
		}
		return m.workspaceCommand(resource, action, target, target, nil, false), nil
	}
	name, err := state.workspaceName()
	if err != nil {
		return nil, err
	}
	variables := map[string]string(nil)
	changed := false
	if state.hasVariables() {
		variables, err = state.variables()
		if err != nil {
			return nil, err
		}
		changed = state.variablesChanged(variables)
	}
	if action == "rename" && target == "" {
		return nil, fmt.Errorf("no %s selected", resource)
	}
	return m.workspaceCommand(resource, action, target, name, variables, changed), nil
}

func (m Model) confirmWorkspaceDelete() (tea.Cmd, error) {
	return m.commitWorkspaceModal()
}

func (m Model) updateWorkspaceModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.workspaceModal == nil {
		return m, nil
	}
	state := m.workspaceModal
	if msg.Type == tea.KeyEscape {
		m.workspaceModal = nil
		return m, nil
	}
	if state.kind == workspaceCollectionDelete || state.kind == workspaceEnvironmentDelete {
		if workspaceKey(msg, tea.KeyUp, 'k') || workspaceKey(msg, tea.KeyLeft, 'h') {
			state.moveTarget(-1)
			return m, nil
		}
		if workspaceKey(msg, tea.KeyDown, 'j') || workspaceKey(msg, tea.KeyRight, 'l') {
			state.moveTarget(1)
			return m, nil
		}
		if msg.Type == tea.KeyEnter || (msg.Type == tea.KeyRunes && (string(msg.Runes) == "y" || string(msg.Runes) == "Y")) {
			cmd, err := m.confirmWorkspaceDelete()
			if err != nil {
				state.err = err.Error()
				return m, nil
			}
			m.workspaceModal = nil
			return m, cmd
		}
		if msg.Type == tea.KeyRunes && (string(msg.Runes) == "n" || string(msg.Runes) == "N") {
			m.workspaceModal = nil
		}
		return m, nil
	}
	if msg.Type == tea.KeyEnter || msg.Type == tea.KeyCtrlS {
		if state.focus == workspaceFocusTarget {
			state.focus = workspaceFocusName
			state.focusActiveInput()
			return m, nil
		}
		cmd, err := m.commitWorkspaceModal()
		if err != nil {
			state.err = err.Error()
			return m, nil
		}
		m.workspaceModal = nil
		return m, cmd
	}
	selectedBefore := state.selected
	cmd := state.update(msg)
	if selectedBefore != state.selected {
		state.syncSelectedEnvironment(m)
	}
	return m, cmd
}

func (m Model) workspaceModalContent() string {
	state := m.workspaceModal
	if state == nil {
		return ""
	}
	lines := []string{titleStyle.Render(state.title), ""}
	if state.hasTarget() {
		lines = append(lines, mutedStyle.Render("j/k choose · Tab next · Enter continue · Esc cancel"), "")
		start, end := workspaceVisibleRange(len(state.targets), state.selected, 8)
		if start > 0 {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("  … %d above", start)))
		}
		for i := start; i < end; i++ {
			target := state.targets[i]
			marker := "  "
			if i == state.selected {
				marker = lipgloss.NewStyle().Foreground(colAccent).Render("▎ ")
			}
			lines = append(lines, marker+valueStyle.Render(target))
		}
		if end < len(state.targets) {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("  … %d below", len(state.targets)-end)))
		}
		if state.kind == workspaceCollectionDelete || state.kind == workspaceEnvironmentDelete {
			lines = append(lines, "", errStyle.Render(fmt.Sprintf("Delete %q?", state.selectedTarget())), mutedStyle.Render("Enter/y confirm · n/Esc cancel"))
		}
	}
	if state.hasName() {
		if state.hasTarget() {
			lines = append(lines, "")
		}
		lines = append(lines, mutedStyle.Render("Tab next field · Enter save · Esc cancel"), workspaceModalField("Name", state.name.View(), state.focus == workspaceFocusName))
	}
	if state.hasVariables() {
		lines = append(lines, "", mutedStyle.Render("Variables · Tab key/value · Ctrl+N add · Ctrl+D remove"))
		start, end := workspaceVisibleRange(len(state.rows), state.row, 8)
		if start > 0 {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("  … %d above", start)))
		}
		for i := start; i < end; i++ {
			row := state.rows[i]
			marker := "  "
			if i == state.row {
				marker = lipgloss.NewStyle().Foreground(colAccent).Render("▎ ")
			}
			lines = append(lines, marker+row.key.View()+punctStyle.Render(" : ")+row.value.View())
		}
		if end < len(state.rows) {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("  … %d below", len(state.rows)-end)))
		}
	}
	if state.err != "" {
		lines = append(lines, "", errStyle.Render(state.err))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func workspaceVisibleRange(total, selected, limit int) (int, int) {
	if total <= limit || limit <= 0 {
		return 0, total
	}
	start := selected - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > total {
		start = total - limit
	}
	return start, start + limit
}

func workspaceModalField(label, value string, focused bool) string {
	marker := "  "
	if focused {
		marker = lipgloss.NewStyle().Foreground(colAccent).Render("▎ ")
	}
	return marker + keyStyle.Render(label) + punctStyle.Render(" : ") + value
}

func (m Model) workspaceModalView() string {
	cardWidth := m.width - 8
	if cardWidth < 36 {
		cardWidth = 36
	}
	if cardWidth > 86 {
		cardWidth = 86
	}
	card := borderStyle(true).Width(cardWidth).Padding(1, 2).Render(m.workspaceModalContent())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}
