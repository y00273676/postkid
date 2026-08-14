package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"go.planetmeican.com/yangguang/postkid/internal/app"
	"go.planetmeican.com/yangguang/postkid/internal/model"
)

const (
	grpcFocusCollection = iota
	grpcFocusName
	grpcFocusTarget
	grpcFocusService
	grpcFocusMethod
	grpcFocusDescriptorSource
	grpcFocusProtoFiles
	grpcFocusImportPaths
	grpcFocusDescriptorSet
	grpcFocusTLS
	grpcFocusServerName
	grpcFocusInsecureSkip
	grpcFocusMetadata
)

// grpcDescriptorSource controls where the protobuf descriptors used for
// discovery and invocation come from. Empty descriptor fields mean reflection
// so existing gRPC request YAML remains compatible.
type grpcDescriptorSource uint8

const (
	grpcDescriptorReflection grpcDescriptorSource = iota
	grpcDescriptorProtoFiles
	grpcDescriptorSet
)

func (s grpcDescriptorSource) String() string {
	switch s {
	case grpcDescriptorProtoFiles:
		return "proto files"
	case grpcDescriptorSet:
		return "protoset"
	default:
		return "reflection"
	}
}

// grpcModalState is deliberately independent from modalState. HTTP's method
// and URL form has validation rules that do not apply to gRPC, while both
// forms still reuse the same text input and key/value row helpers.
type grpcModalState struct {
	edit             bool
	collection       int
	focus            int
	metadataRow      int
	metadataCol      int
	name             textinput.Model
	target           textinput.Model
	service          textinput.Model
	method           textinput.Model
	descriptorSource grpcDescriptorSource
	protoFiles       textinput.Model
	importPaths      textinput.Model
	descriptorSet    textinput.Model
	serverName       textinput.Model
	tls              bool
	insecureSkip     bool
	rows             []kvEditRow
	services         []model.GRPCService
	serviceIndex     int
	methodIndex      int
	discovering      bool
	err              string
}

func newGRPCModal(colls []*model.Collection, selected *model.Collection, req *model.Request, edit bool) *grpcModalState {
	state := &grpcModalState{
		edit:          edit,
		collection:    selectedCollectionIndex(colls, selected),
		name:          newInput("", "request-name"),
		target:        newInput("", "host:port or grpcs://host:port"),
		service:       newInput("", "package.Service"),
		method:        newInput("", "Method"),
		protoFiles:    newInput("", "proto/a.proto, proto/b.proto"),
		importPaths:   newInput("", "proto, third_party"),
		descriptorSet: newInput("", "descriptors.pb"),
		serverName:    newInput("", "TLS server name (optional)"),
		rows:          sortedKVRows(nil),
	}
	if req != nil {
		state.name.SetValue(req.Name)
		state.target.SetValue(req.URL)
		state.service.SetValue(grpcService(*req))
		state.method.SetValue(grpcMethod(*req))
		if req.GRPC != nil {
			state.protoFiles.SetValue(strings.Join(req.GRPC.ProtoFiles, ", "))
			state.importPaths.SetValue(strings.Join(req.GRPC.ImportPaths, ", "))
			state.descriptorSet.SetValue(req.GRPC.DescriptorSet)
			switch {
			case strings.TrimSpace(req.GRPC.DescriptorSet) != "":
				state.descriptorSource = grpcDescriptorSet
			case len(req.GRPC.ProtoFiles) > 0:
				state.descriptorSource = grpcDescriptorProtoFiles
			default:
				state.descriptorSource = grpcDescriptorReflection
			}
		}
		state.rows = sortedKVRows(grpcMetadata(req))
		if req.GRPC != nil && req.GRPC.TLS != nil {
			state.tls = req.GRPC.TLS.Enabled
			state.insecureSkip = req.GRPC.TLS.InsecureSkipVerify
			state.serverName.SetValue(req.GRPC.TLS.ServerName)
		}
	}
	if edit {
		state.focus = grpcFocusTarget
	} else {
		state.focus = grpcFocusCollection
	}
	state.resize(100)
	state.focusInputs()
	return state
}

// GRPCService/GRPCMethod keep the form tolerant of both representations that
// the core accepts: a nested grpc block or a fully-qualified method string.
func grpcService(r model.Request) string {
	if r.GRPC != nil && strings.TrimSpace(r.GRPC.Service) != "" {
		return r.GRPC.Service
	}
	method := strings.Trim(strings.TrimSpace(r.Method), "/")
	if index := strings.LastIndex(method, "/"); index > 0 {
		return strings.Trim(method[:index], "/")
	}
	return ""
}

func grpcMethod(r model.Request) string {
	if r.GRPC != nil && strings.TrimSpace(r.GRPC.Method) != "" {
		return r.GRPC.Method
	}
	method := strings.Trim(strings.TrimSpace(r.Method), "/")
	if index := strings.LastIndex(method, "/"); index >= 0 {
		return method[index+1:]
	}
	return method
}

func selectedCollectionIndex(colls []*model.Collection, selected *model.Collection) int {
	if selected == nil {
		return 0
	}
	for i, coll := range colls {
		if coll != nil && coll.FilePath == selected.FilePath && coll.Name == selected.Name {
			return i
		}
	}
	return 0
}

func grpcMetadata(req *model.Request) map[string]string {
	if req == nil {
		return nil
	}
	if req.GRPC != nil && len(req.GRPC.Metadata) > 0 {
		return cloneStringMap(req.GRPC.Metadata)
	}
	return cloneStringMap(req.Headers)
}

func (m *grpcModalState) fields() []int {
	if m.edit {
		fields := []int{grpcFocusTarget, grpcFocusService, grpcFocusMethod, grpcFocusDescriptorSource}
		fields = m.appendDescriptorFields(fields)
		fields = append(fields, grpcFocusTLS)
		if m.tls {
			fields = append(fields, grpcFocusServerName, grpcFocusInsecureSkip)
		}
		return append(fields, grpcFocusMetadata)
	}
	fields := []int{grpcFocusCollection, grpcFocusName, grpcFocusTarget, grpcFocusService, grpcFocusMethod, grpcFocusDescriptorSource}
	fields = m.appendDescriptorFields(fields)
	fields = append(fields, grpcFocusTLS)
	if m.tls {
		fields = append(fields, grpcFocusServerName, grpcFocusInsecureSkip)
	}
	return append(fields, grpcFocusMetadata)
}

func (m *grpcModalState) appendDescriptorFields(fields []int) []int {
	switch m.descriptorSource {
	case grpcDescriptorProtoFiles:
		return append(fields, grpcFocusProtoFiles, grpcFocusImportPaths)
	case grpcDescriptorSet:
		return append(fields, grpcFocusDescriptorSet)
	default:
		return fields
	}
}

func (m *grpcModalState) currentField() int {
	fields := m.fields()
	if len(fields) == 0 {
		return grpcFocusTarget
	}
	if m.focus < 0 {
		m.focus = 0
	}
	if m.focus >= len(fields) {
		m.focus = len(fields) - 1
	}
	return fields[m.focus]
}

func (m *grpcModalState) resize(screenWidth int) {
	if m == nil {
		return
	}
	w := screenWidth - 24
	if w < 24 {
		w = 24
	}
	if w > 76 {
		w = 76
	}
	for _, input := range []*textinput.Model{&m.name, &m.target, &m.service, &m.method, &m.protoFiles, &m.importPaths, &m.descriptorSet, &m.serverName} {
		input.Width = w
	}
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
}

func (m *grpcModalState) focusInputs() {
	for _, input := range []*textinput.Model{&m.name, &m.target, &m.service, &m.method, &m.protoFiles, &m.importPaths, &m.descriptorSet, &m.serverName} {
		input.Blur()
	}
	for i := range m.rows {
		m.rows[i].key.Blur()
		m.rows[i].value.Blur()
	}
	switch m.currentField() {
	case grpcFocusName:
		m.name.Focus()
	case grpcFocusTarget:
		m.target.Focus()
	case grpcFocusService:
		m.service.Focus()
	case grpcFocusMethod:
		m.method.Focus()
	case grpcFocusProtoFiles:
		m.protoFiles.Focus()
	case grpcFocusImportPaths:
		m.importPaths.Focus()
	case grpcFocusDescriptorSet:
		m.descriptorSet.Focus()
	case grpcFocusServerName:
		m.serverName.Focus()
	case grpcFocusMetadata:
		if len(m.rows) == 0 {
			m.rows = sortedKVRows(nil)
		}
		if m.metadataRow < 0 || m.metadataRow >= len(m.rows) {
			m.metadataRow = 0
		}
		if m.metadataCol == 0 {
			m.rows[m.metadataRow].key.Focus()
		} else {
			m.rows[m.metadataRow].value.Focus()
		}
	}
}

func (m *grpcModalState) update(msg tea.KeyMsg) tea.Cmd {
	if isTab(msg) {
		fields := m.fields()
		if isReverseTab(msg) {
			m.focus--
			if m.focus < 0 {
				m.focus = len(fields) - 1
			}
		} else {
			m.focus = (m.focus + 1) % len(fields)
		}
		m.focusInputs()
		return nil
	}
	field := m.currentField()
	if field == grpcFocusMetadata {
		// Keep Tab for leaving the metadata editor. The shared KV editor still
		// provides Ctrl+N/Ctrl+D and arrow navigation inside the rows.
		tmp := modalState{rows: m.rows, row: m.metadataRow, col: m.metadataCol}
		cmd := tmp.updateKV(msg)
		m.rows, m.metadataRow, m.metadataCol = tmp.rows, tmp.row, tmp.col
		return cmd
	}
	if field == grpcFocusCollection {
		if msg.Type == tea.KeyUp || msg.Type == tea.KeyLeft {
			m.collection--
			if m.collection < 0 {
				m.collection = 0
			}
			return nil
		}
		if msg.Type == tea.KeyDown || msg.Type == tea.KeyRight {
			m.collection++
			return nil
		}
	}
	if field == grpcFocusTLS {
		if msg.Type == tea.KeySpace || msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight {
			m.tls = !m.tls
			if !m.tls && m.currentField() == grpcFocusInsecureSkip {
				m.focus--
			}
			m.focusInputs()
			return nil
		}
	}
	if field == grpcFocusInsecureSkip {
		if msg.Type == tea.KeySpace || msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight {
			m.insecureSkip = !m.insecureSkip
			return nil
		}
	}
	if field == grpcFocusDescriptorSource {
		if msg.Type == tea.KeySpace || msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight {
			delta := 1
			if msg.Type == tea.KeyLeft {
				delta = -1
			}
			m.descriptorSource = grpcDescriptorSource(wrapIndex(int(m.descriptorSource)+delta, 3))
			m.services = nil
			m.serviceIndex = 0
			m.methodIndex = 0
			m.err = ""
			m.focusInputs()
			return nil
		}
	}
	if field == grpcFocusService && len(m.services) > 0 {
		if msg.Type == tea.KeyUp || msg.Type == tea.KeyDown {
			delta := 1
			if msg.Type == tea.KeyUp {
				delta = -1
			}
			m.serviceIndex = wrapIndex(m.serviceIndex+delta, len(m.services))
			m.selectService()
			return nil
		}
	}
	if field == grpcFocusMethod {
		if service := m.selectedService(); service != nil && len(service.Methods) > 0 && (msg.Type == tea.KeyUp || msg.Type == tea.KeyDown) {
			delta := 1
			if msg.Type == tea.KeyUp {
				delta = -1
			}
			m.methodIndex = wrapIndex(m.methodIndex+delta, len(service.Methods))
			m.selectMethod()
			return nil
		}
	}
	var cmd tea.Cmd
	switch field {
	case grpcFocusName:
		m.name, cmd = m.name.Update(msg)
	case grpcFocusTarget:
		m.target, cmd = m.target.Update(msg)
	case grpcFocusService:
		m.service, cmd = m.service.Update(msg)
	case grpcFocusMethod:
		m.method, cmd = m.method.Update(msg)
	case grpcFocusProtoFiles:
		m.protoFiles, cmd = m.protoFiles.Update(msg)
	case grpcFocusImportPaths:
		m.importPaths, cmd = m.importPaths.Update(msg)
	case grpcFocusDescriptorSet:
		m.descriptorSet, cmd = m.descriptorSet.Update(msg)
	case grpcFocusServerName:
		m.serverName, cmd = m.serverName.Update(msg)
	}
	return cmd
}

func wrapIndex(index, length int) int {
	if length <= 0 {
		return 0
	}
	for index < 0 {
		index += length
	}
	return index % length
}

func (m *grpcModalState) selectedService() *model.GRPCService {
	if m.serviceIndex < 0 || m.serviceIndex >= len(m.services) {
		return nil
	}
	return &m.services[m.serviceIndex]
}

func (m *grpcModalState) selectService() {
	service := m.selectedService()
	if service == nil {
		return
	}
	m.service.SetValue(service.Name)
	m.methodIndex = 0
	m.method.SetValue("")
	m.selectMethod()
	m.focusInputs()
}

func (m *grpcModalState) selectMethod() {
	service := m.selectedService()
	if service == nil || m.methodIndex < 0 || m.methodIndex >= len(service.Methods) {
		return
	}
	m.method.SetValue(service.Methods[m.methodIndex].Name)
	m.focusInputs()
}

func (m *grpcModalState) metadata() map[string]string {
	values := make(map[string]string)
	seen := make(map[string]string)
	for _, row := range m.rows {
		key := strings.TrimSpace(row.key.Value())
		value := row.value.Value()
		if key == "" && strings.TrimSpace(value) == "" {
			continue
		}
		if key == "" {
			continue
		}
		folded := strings.ToLower(key)
		if previous, exists := seen[folded]; exists {
			// The caller performs the user-facing validation with the original
			// spelling; this map is only for discovery/send fallback paths.
			_ = previous
			continue
		}
		seen[folded] = key
		values[key] = value
	}
	return values
}

// splitCommaValues is intentionally forgiving about whitespace so paths can
// be edited as a compact comma-separated field. Empty and duplicate entries
// are ignored before writing YAML.
func splitCommaValues(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		values = append(values, part)
	}
	return values
}

func (m *grpcModalState) descriptorValues() (protoFiles, importPaths []string, descriptorSet string) {
	switch m.descriptorSource {
	case grpcDescriptorProtoFiles:
		return splitCommaValues(m.protoFiles.Value()), splitCommaValues(m.importPaths.Value()), ""
	case grpcDescriptorSet:
		return nil, nil, strings.TrimSpace(m.descriptorSet.Value())
	default:
		return nil, nil, ""
	}
}

// discoveryRequest builds the minimal gRPC definition needed by discovery.
// Service and method intentionally remain empty: discovery is the operation
// that supplies those values. The application resolver still handles target,
// metadata, TLS, variables, and collection-relative descriptor paths.
func (m *grpcModalState) discoveryRequest(existing *model.Request) model.Request {
	var req model.Request
	if existing != nil {
		req = *existing
		req.Headers = nil
		req.Params = nil
		req.Body = ""
		req.Variables = cloneStringMap(existing.Variables)
	}
	protoFiles, importPaths, descriptorSet := m.descriptorValues()
	grpc := &model.GRPCRequest{
		Metadata:      m.metadata(),
		ProtoFiles:    protoFiles,
		ImportPaths:   importPaths,
		DescriptorSet: descriptorSet,
	}
	if m.tls {
		tlsConfig := model.GRPCTLSConfig{}
		if existing != nil && existing.GRPC != nil && existing.GRPC.TLS != nil {
			tlsConfig = *existing.GRPC.TLS
		}
		tlsConfig.Enabled = true
		tlsConfig.ServerName = strings.TrimSpace(m.serverName.Value())
		tlsConfig.InsecureSkipVerify = m.insecureSkip
		grpc.TLS = &tlsConfig
	}
	req.Name = "discovery"
	req.Protocol = model.ProtocolGRPC
	req.Method = ""
	req.URL = strings.TrimSpace(m.target.Value())
	req.GRPC = grpc
	return req
}

func (m *grpcModalState) request(existing *model.Request) (model.Request, error) {
	if m == nil {
		return model.Request{}, fmt.Errorf("gRPC form is not open")
	}
	name := strings.TrimSpace(m.name.Value())
	if existing != nil {
		name = existing.Name
	}
	if name == "" {
		return model.Request{}, fmt.Errorf("request name cannot be empty")
	}
	target := strings.TrimSpace(m.target.Value())
	if target == "" {
		return model.Request{}, fmt.Errorf("gRPC target cannot be empty")
	}
	service := strings.TrimSpace(m.service.Value())
	method := strings.TrimSpace(m.method.Value())
	if service == "" || method == "" {
		return model.Request{}, fmt.Errorf("gRPC service and method are required")
	}
	metadataValues := make(map[string]string)
	seen := make(map[string]string)
	for _, row := range m.rows {
		key := strings.TrimSpace(row.key.Value())
		value := row.value.Value()
		if key == "" && strings.TrimSpace(value) == "" {
			continue
		}
		if key == "" {
			return model.Request{}, fmt.Errorf("metadata key cannot be empty")
		}
		folded := strings.ToLower(key)
		if previous, exists := seen[folded]; exists {
			return model.Request{}, fmt.Errorf("duplicate metadata %q conflicts with %q", key, previous)
		}
		seen[folded] = key
		metadataValues[key] = value
	}
	protoFiles, importPaths, descriptorSet := m.descriptorValues()
	if descriptorSet != "" && len(protoFiles) > 0 {
		return model.Request{}, fmt.Errorf("descriptor_set cannot be combined with proto_files")
	}
	grpc := &model.GRPCRequest{
		Service: service, Method: method, Metadata: metadataValues,
		ProtoFiles: protoFiles, ImportPaths: importPaths, DescriptorSet: descriptorSet,
	}
	if m.tls {
		tlsConfig := model.GRPCTLSConfig{}
		if existing != nil && existing.GRPC != nil && existing.GRPC.TLS != nil {
			tlsConfig = *existing.GRPC.TLS
		}
		tlsConfig.Enabled = true
		tlsConfig.ServerName = strings.TrimSpace(m.serverName.Value())
		tlsConfig.InsecureSkipVerify = m.insecureSkip
		grpc.TLS = &tlsConfig
	}
	var req model.Request
	if existing != nil {
		req = *existing
		req.Headers = cloneStringMap(existing.Headers)
		req.Params = cloneStringMap(existing.Params)
		req.Variables = cloneStringMap(existing.Variables)
	}
	req.Name = name
	req.Protocol = model.ProtocolGRPC
	req.Method = method
	req.URL = target
	req.GRPC = grpc
	if existing != nil {
		req.Body = existing.Body
	}
	if err := app.ValidateGRPCRequest(req); err != nil {
		return model.Request{}, err
	}
	return req, nil
}

func (m Model) grpcModalView() string {
	cardWidth := m.width - 8
	if cardWidth < 42 {
		cardWidth = 42
	}
	if cardWidth > 100 {
		cardWidth = 100
	}
	card := borderStyle(true).Width(cardWidth).Padding(1, 2).Render(m.grpcModalContent())
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

func (m Model) grpcModalContent() string {
	state := m.grpcModal
	if state == nil {
		return ""
	}
	lines := []string{titleStyle.Render(map[bool]string{true: "Edit gRPC Request", false: "New gRPC Request"}[state.edit]), ""}
	lines = append(lines, mutedStyle.Render("Tab fields · Ctrl+G discover descriptors · Enter save · Esc cancel"), "")
	if !state.edit {
		collName := "(no collections)"
		if state.collection >= 0 && state.collection < len(m.colls) {
			collName = m.colls[state.collection].Name
		}
		line := keyStyle.Render("Collection") + punctStyle.Render(" : ") + valueStyle.Render(collName)
		if state.currentField() == grpcFocusCollection {
			line = lipgloss.NewStyle().Foreground(colAccent).Render("▎ ") + line
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
		lines = append(lines, modalField("Name", state.name.View(), state.currentField() == grpcFocusName))
	}
	lines = append(lines,
		modalField("Target", state.target.View(), state.currentField() == grpcFocusTarget),
		modalField("Service", state.service.View(), state.currentField() == grpcFocusService),
		modalField("Method", state.method.View(), state.currentField() == grpcFocusMethod),
	)
	lines = append(lines, modalField("Descriptors", valueStyle.Render("["+state.descriptorSource.String()+"] (Space/←/→ to change)"), state.currentField() == grpcFocusDescriptorSource))
	switch state.descriptorSource {
	case grpcDescriptorProtoFiles:
		lines = append(lines,
			modalField("Proto files", state.protoFiles.View(), state.currentField() == grpcFocusProtoFiles),
			modalField("Import paths", state.importPaths.View(), state.currentField() == grpcFocusImportPaths),
		)
	case grpcDescriptorSet:
		lines = append(lines, modalField("Descriptor set", state.descriptorSet.View(), state.currentField() == grpcFocusDescriptorSet))
	}
	tlsValue := "plaintext"
	if state.tls {
		tlsValue = "TLS"
	}
	lines = append(lines, modalField("Transport", valueStyle.Render("["+tlsValue+"] (Space to toggle)"), state.currentField() == grpcFocusTLS))
	if state.tls {
		lines = append(lines, modalField("Server name", state.serverName.View(), state.currentField() == grpcFocusServerName))
		verify := "verify certificate"
		if state.insecureSkip {
			verify = "skip certificate verification"
		}
		lines = append(lines, modalField("TLS verify", valueStyle.Render("["+verify+"] (Space to toggle)"), state.currentField() == grpcFocusInsecureSkip))
	}
	if state.currentField() == grpcFocusMetadata {
		lines = append(lines, keyStyle.Render("Metadata")+punctStyle.Render(" : ")+mutedStyle.Render("Ctrl+N add · Ctrl+D remove"))
		for i, row := range state.rows {
			if i > 4 {
				break
			}
			marker := "  "
			if i == state.metadataRow {
				marker = lipgloss.NewStyle().Foreground(colAccent).Render("▎ ")
			}
			lines = append(lines, marker+row.key.View()+punctStyle.Render(" : ")+row.value.View())
		}
	} else {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("Metadata: %d entries", len(state.metadata()))))
	}
	if len(state.services) > 0 {
		lines = append(lines, "", keyStyle.Render(state.descriptorSource.String()+" services / methods"))
		for i, service := range state.services {
			if i > 4 {
				break
			}
			marker := "  "
			if i == state.serviceIndex {
				marker = lipgloss.NewStyle().Foreground(colAccent).Render("▎ ")
			}
			lines = append(lines, marker+valueStyle.Render(service.Name)+mutedStyle.Render(fmt.Sprintf(" (%d methods)", len(service.Methods))))
		}
		if service := state.selectedService(); service != nil {
			for i, method := range service.Methods {
				if i > 4 {
					break
				}
				marker := "    "
				if i == state.methodIndex {
					marker = lipgloss.NewStyle().Foreground(colAccent).Render("  ▎ ")
				}
				suffix := ""
				if method.ClientStreaming || method.ServerStreaming {
					suffix = " [streaming — send unsupported]"
				}
				lines = append(lines, marker+valueStyle.Render(method.Name)+mutedStyle.Render(suffix))
			}
		}
	}
	if state.discovering {
		lines = append(lines, "", mutedStyle.Render("discovering "+state.descriptorSource.String()+"…"))
	}
	if state.err != "" {
		lines = append(lines, "", errStyle.Render(state.err))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
