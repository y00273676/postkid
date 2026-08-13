package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"go.planetmeican.com/yangguang/postkid/internal/curlimport"
	"go.planetmeican.com/yangguang/postkid/internal/model"
)

type curlImportPhase uint8

const (
	curlImportInput curlImportPhase = iota + 1
	curlImportPreview
	curlImportTarget
)

const (
	curlImportFocusCollection = iota
	curlImportFocusName
)

// curlImportRequest is kept as an alias for package-local callers while
// retaining every field of model.Request, including authentication fields.
type curlImportRequest = model.Request

type curlImportModalState struct {
	phase curlImportPhase

	// Multiline input is intentionally a plain rune buffer. Enter inserts a
	// newline; Ctrl+S is the only parse/submit shortcut. Paste messages carry
	// all bracketed-paste runes in one KeyMsg and are inserted unchanged.
	input  []rune
	cursor int

	// Keep the parser result as a complete model.Request. In particular, a
	// cURL -u credential must survive the preview/save round trip instead of
	// being reduced to only the fields shown in the preview.
	parsed   curlImportRequest
	parseErr string

	collectionIndex int
	focus           int
	requestName     textinput.Model
	saveErr         string
}

func newCurlImportModal(m Model) *curlImportModalState {
	state := &curlImportModalState{
		phase:       curlImportInput,
		focus:       curlImportFocusCollection,
		requestName: curlImportInputField("imported-request"),
	}
	if len(m.colls) == 0 {
		state.saveErr = "no collection available"
	}
	return state
}

func curlImportInputField(value string) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.PromptStyle = valueStyle
	in.TextStyle = valueStyle
	in.Placeholder = "request-name"
	in.CharLimit = 0
	in.Width = 42
	in.SetValue(value)
	return in
}

func (m *curlImportModalState) insertRunes(runes []rune) {
	if len(runes) == 0 {
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.input) {
		m.cursor = len(m.input)
	}
	m.input = append(m.input, make([]rune, len(runes))...)
	copy(m.input[m.cursor+len(runes):], m.input[m.cursor:])
	copy(m.input[m.cursor:], runes)
	m.cursor += len(runes)
}

func (m *curlImportModalState) deleteBeforeCursor() {
	if m.cursor <= 0 || len(m.input) == 0 {
		return
	}
	m.input = append(m.input[:m.cursor-1], m.input[m.cursor:]...)
	m.cursor--
}

func (m *curlImportModalState) deleteAtCursor() {
	if m.cursor < 0 || m.cursor >= len(m.input) {
		return
	}
	m.input = append(m.input[:m.cursor], m.input[m.cursor+1:]...)
}

func (m *curlImportModalState) moveCursor(delta int) {
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.input) {
		m.cursor = len(m.input)
	}
}

func (m *curlImportModalState) cursorLineStart() {
	for m.cursor > 0 && m.input[m.cursor-1] != '\n' {
		m.cursor--
	}
}

func (m *curlImportModalState) cursorLineEnd() {
	for m.cursor < len(m.input) && m.input[m.cursor] != '\n' {
		m.cursor++
	}
}

func (m *curlImportModalState) parse() {
	parsed, err := curlimport.Parse(string(m.input))
	if err != nil {
		m.parseErr = err.Error()
		return
	}
	parsed.Headers = cloneStringMap(parsed.Headers)
	m.parsed = parsed
	m.parseErr = ""
	m.saveErr = ""
	m.phase = curlImportPreview
}

func cloneStringMap(values map[string]string) map[string]string {
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func (m *curlImportModalState) startTarget(collectionCount int) {
	if collectionCount == 0 {
		m.saveErr = "no collection available"
		return
	}
	m.phase = curlImportTarget
	m.collectionIndex = min(max(m.collectionIndex, 0), collectionCount-1)
	m.focus = curlImportFocusCollection
	m.requestName.Focus()
	m.requestName.Blur()
}

func (m *curlImportModalState) moveCollection(delta, count int) {
	if count == 0 {
		return
	}
	m.collectionIndex = (m.collectionIndex + delta + count) % count
}

func (m *curlImportModalState) focusTargetInput() {
	if m.focus == curlImportFocusName {
		m.requestName.Focus()
	} else {
		m.requestName.Blur()
	}
}

func (m *curlImportModalState) updateInput(msg tea.KeyMsg) {
	if msg.Paste && msg.Type == tea.KeyRunes {
		m.insertRunes(msg.Runes)
		return
	}
	switch msg.Type {
	case tea.KeyEnter:
		m.insertRunes([]rune{'\n'})
	case tea.KeyBackspace:
		m.deleteBeforeCursor()
	case tea.KeyDelete:
		m.deleteAtCursor()
	case tea.KeyLeft:
		m.moveCursor(-1)
	case tea.KeyRight:
		m.moveCursor(1)
	case tea.KeyUp:
		m.moveCursorVertical(-1)
	case tea.KeyDown:
		m.moveCursorVertical(1)
	case tea.KeyHome:
		m.cursorLineStart()
	case tea.KeyEnd:
		m.cursorLineEnd()
	case tea.KeyCtrlA:
		m.cursorLineStart()
	case tea.KeyCtrlE:
		m.cursorLineEnd()
	case tea.KeyRunes:
		m.insertRunes(msg.Runes)
	}
}

func (m *curlImportModalState) moveCursorVertical(direction int) {
	lineStart := m.cursor
	for lineStart > 0 && m.input[lineStart-1] != '\n' {
		lineStart--
	}
	column := m.cursor - lineStart
	if direction < 0 {
		if lineStart == 0 {
			return
		}
		previousEnd := lineStart - 1
		previousStart := previousEnd
		for previousStart > 0 && m.input[previousStart-1] != '\n' {
			previousStart--
		}
		m.cursor = min(previousStart+column, previousEnd)
		return
	}
	lineEnd := m.cursor
	for lineEnd < len(m.input) && m.input[lineEnd] != '\n' {
		lineEnd++
	}
	if lineEnd == len(m.input) {
		return
	}
	nextStart := lineEnd + 1
	nextEnd := nextStart
	for nextEnd < len(m.input) && m.input[nextEnd] != '\n' {
		nextEnd++
	}
	m.cursor = min(nextStart+column, nextEnd)
}

func (m *curlImportModalState) inputView(width, height int) string {
	if width < 8 {
		width = 8
	}
	if height < 1 {
		height = 1
	}
	withCursor := make([]rune, len(m.input))
	copy(withCursor, m.input)
	if m.cursor >= len(withCursor) {
		withCursor = append(withCursor, '▌')
	} else {
		withCursor = append(withCursor[:m.cursor], append([]rune{'▌'}, withCursor[m.cursor:]...)...)
	}
	lines := strings.Split(string(withCursor), "\n")
	// Keep the cursor's neighborhood visible for pasted multi-line commands.
	line := 0
	position := 0
	for i, r := range withCursor {
		if i >= m.cursor {
			break
		}
		if r == '\n' {
			line++
			position = 0
		} else {
			position++
		}
	}
	start := 0
	if len(lines) > height {
		start = max(0, line-height+1)
		if start+height > len(lines) {
			start = len(lines) - height
		}
	}
	visible := lines[start:min(start+height, len(lines))]
	for i := range visible {
		visible[i] = truncate(visible[i], width)
	}
	_ = position
	return strings.Join(visible, "\n")
}

func sensitiveCurlHeader(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, token := range []string{"authorization", "cookie", "token", "secret", "password", "api-key", "apikey", "proxy-authorization"} {
		if strings.Contains(key, token) {
			return true
		}
	}
	return false
}

func maskedCurlHeaders(headers map[string]string) map[string]string {
	masked := make(map[string]string, len(headers))
	for key, value := range headers {
		if sensitiveCurlHeader(key) && value != "" {
			masked[key] = "<redacted>"
		} else {
			masked[key] = value
		}
	}
	return masked
}

func sortedCurlHeaderKeys(headers map[string]string) []string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (m *curlImportModalState) importedRequest() model.Request {
	request := m.parsed
	request.Name = strings.TrimSpace(m.requestName.Value())
	request.Method = strings.ToUpper(strings.TrimSpace(request.Method))
	request.URL = strings.TrimSpace(request.URL)
	request.Headers = cloneStringMap(request.Headers)
	return request
}

func (m *curlImportModalState) validateTarget(collectionCount int) error {
	if collectionCount == 0 {
		return fmt.Errorf("no collection available")
	}
	if m.collectionIndex < 0 || m.collectionIndex >= collectionCount {
		return fmt.Errorf("invalid collection selection")
	}
	name := strings.TrimSpace(m.requestName.Value())
	if name == "" {
		return fmt.Errorf("request name cannot be empty")
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return fmt.Errorf("request name %q contains invalid characters", name)
		}
	}
	if m.parsed.Method == "" || m.parsed.URL == "" {
		return fmt.Errorf("parsed request is incomplete")
	}
	return nil
}

func (m *Model) openCurlImportModal() {
	m.curlImport = newCurlImportModal(*m)
	m.err = nil
}

func (m Model) saveCurlImportCmd(state *curlImportModalState) tea.Cmd {
	appModel := m
	request := state.importedRequest()
	collectionIndex := state.collectionIndex
	return func() tea.Msg {
		collections := appModel.app.Collections()
		if collectionIndex < 0 || collectionIndex >= len(collections) {
			return CurlImportSaveFailedMsg{Err: fmt.Errorf("invalid collection selection")}
		}
		collection := collections[collectionIndex]
		if err := appModel.app.AddRequest(&collection, &request); err != nil {
			return CurlImportSaveFailedMsg{Err: err}
		}
		return CurlImportSavedMsg{Collection: collection.Name, Name: request.Name}
	}
}

func (m Model) updateCurlImport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := m.curlImport
	if state == nil {
		return m, nil
	}
	if msg.Type == tea.KeyEscape {
		m.curlImport = nil
		return m, nil
	}
	switch state.phase {
	case curlImportInput:
		if msg.Type == tea.KeyCtrlS {
			state.parse()
			return m, nil
		}
		state.updateInput(msg)
		return m, nil
	case curlImportPreview:
		if msg.Type == tea.KeyCtrlS || msg.Type == tea.KeyEnter || msg.Type == tea.KeyTab {
			state.startTarget(len(m.colls))
		}
		return m, nil
	case curlImportTarget:
		if msg.Type == tea.KeyCtrlS {
			if err := state.validateTarget(len(m.colls)); err != nil {
				state.saveErr = err.Error()
				return m, nil
			}
			state.saveErr = ""
			cmd := m.saveCurlImportCmd(state)
			return m, cmd
		}
		if msg.Type == tea.KeyTab {
			state.focus = (state.focus + 1) % 2
			state.focusTargetInput()
			return m, nil
		}
		if state.focus == curlImportFocusCollection {
			switch {
			case workspaceKey(msg, tea.KeyUp, 'k'), workspaceKey(msg, tea.KeyLeft, 'h'):
				state.moveCollection(-1, len(m.colls))
			case workspaceKey(msg, tea.KeyDown, 'j'), workspaceKey(msg, tea.KeyRight, 'l'):
				state.moveCollection(1, len(m.colls))
			case msg.Type == tea.KeyEnter:
				state.focus = curlImportFocusName
				state.focusTargetInput()
			}
			return m, nil
		}
		var cmd tea.Cmd
		state.requestName, cmd = state.requestName.Update(msg)
		if msg.Type == tea.KeyEnter {
			if err := state.validateTarget(len(m.colls)); err != nil {
				state.saveErr = err.Error()
				return m, nil
			}
			state.saveErr = ""
			cmd = m.saveCurlImportCmd(state)
		}
		return m, cmd
	}
	return m, nil
}

func (m Model) curlImportContent() string {
	state := m.curlImport
	if state == nil {
		return ""
	}
	lines := []string{titleStyle.Render("Import cURL"), ""}
	switch state.phase {
	case curlImportInput:
		lines = append(lines,
			mutedStyle.Render("Paste cURL · Enter newline · Ctrl+S parse · Esc cancel"),
			mutedStyle.Render("Bracketed paste is inserted as-is"),
			"",
			state.inputView(72, 12),
		)
		if state.parseErr != "" {
			lines = append(lines, "", errStyle.Render(state.parseErr))
		}
	case curlImportPreview:
		lines = append(lines, mutedStyle.Render("Preview · Ctrl+S/Enter choose collection · Esc cancel"), "")
		lines = append(lines, modalField("Method", valueStyle.Render(state.parsed.Method), false))
		lines = append(lines, modalField("URL", urlStyle.Render(state.parsed.URL), false), "")
		headerKeys := sortedCurlHeaderKeys(state.parsed.Headers)
		headerLimit := curlImportRowLimit(m.height, 10, 10)
		if len(headerKeys) > headerLimit {
			headerKeys = headerKeys[:headerLimit]
		}
		for _, key := range headerKeys {
			value := state.parsed.Headers[key]
			if sensitiveCurlHeader(key) {
				value = mutedStyle.Render("<redacted>")
			} else {
				value = valueStyle.Render(value)
			}
			lines = append(lines, keyStyle.Render(key)+punctStyle.Render(" : ")+value)
		}
		if len(state.parsed.Headers) > len(headerKeys) {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("… %d more headers", len(state.parsed.Headers)-len(headerKeys))))
		}
		if state.parsed.Body != "" {
			lines = append(lines, "", keyStyle.Render("Body"), truncate(state.parsed.Body, 700))
		}
	case curlImportTarget:
		lines = append(lines, mutedStyle.Render("Choose collection · Tab name · Ctrl+S save · Esc cancel"), "")
		start, end := curlImportVisibleRange(state.collectionIndex, len(m.colls), curlImportRowLimit(m.height, 9, 10))
		if start > 0 {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("… %d collections above", start)))
		}
		for i, coll := range m.colls[start:end] {
			collectionIndex := start + i
			marker := "  "
			if collectionIndex == state.collectionIndex {
				marker = lipgloss.NewStyle().Foreground(colAccent).Render("▎ ")
			}
			lines = append(lines, marker+valueStyle.Render(coll.Name))
		}
		if end < len(m.colls) {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("… %d collections below", len(m.colls)-end)))
		}
		lines = append(lines, "", workspaceModalField("Request name", state.requestName.View(), state.focus == curlImportFocusName))
		if state.saveErr != "" {
			lines = append(lines, "", errStyle.Render(state.saveErr))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// curlImportRowLimit reserves room for the title, instructions, and fields
// around a long list. A zero height is common in unit tests before the first
// WindowSizeMsg, so use a small safe fallback in that case.
func curlImportRowLimit(height, reserved, fallback int) int {
	if height <= 0 {
		return fallback
	}
	limit := height - reserved
	if limit < 1 {
		return 1
	}
	return limit
}

func curlImportVisibleRange(selected, total, limit int) (int, int) {
	if total == 0 {
		return 0, 0
	}
	if limit < 1 || limit >= total {
		return 0, total
	}
	selected = min(max(selected, 0), total-1)
	start := selected - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > total {
		start = total - limit
	}
	return start, start + limit
}

func (m Model) curlImportView() string {
	cardWidth := m.width - 8
	if cardWidth < 42 {
		cardWidth = 42
	}
	if cardWidth > 92 {
		cardWidth = 92
	}
	card := borderStyle(true).Width(cardWidth).Padding(1, 2).Render(m.curlImportContent())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}
