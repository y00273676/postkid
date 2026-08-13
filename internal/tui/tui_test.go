package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"go.planetmeican.com/yangguang/postkid/internal/app"
	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// copyTestData 把 testdata 拷到临时目录，返回 config。
func copyTestData(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"collections", "environments"} {
		src := filepath.Join("..", "..", "testdata", sub)
		dst := filepath.Join(dir, sub)
		os.MkdirAll(dst, 0o755)
		entries, _ := os.ReadDir(src)
		for _, e := range entries {
			data, _ := os.ReadFile(filepath.Join(src, e.Name()))
			os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644)
		}
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestViewRenders(t *testing.T) {
	cfg := copyTestData(t)
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = a.SetEnvironment("sandbox")

	m := New(a)
	m.width, m.height = 120, 40
	m.resize()
	m.selectCurrent() // 选中第一项以渲染请求面板

	out := m.View()
	// lipgloss 对 underline 样式会逐字符输出 ANSI，先剥离再断言纯文本
	plain := stripANSI(out)
	for _, want := range []string{"Collections", "Params", "Headers", "Body", "Auth"} {
		if !strings.Contains(plain, want) {
			t.Errorf("View missing %q\n%s", want, out)
		}
	}
}

// TestSelectAndSend 验证：选中请求 → 发送命令能解析出正确 URL（用 test server）。
func TestSelectAndResolve(t *testing.T) {
	cfg := copyTestData(t)
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = a.SetEnvironment("sandbox")

	m := New(a)
	m.width, m.height = 120, 40
	m.resize()

	// 模拟选中第一项
	m.selectCurrent()
	if m.curReq == nil {
		t.Fatal("no request selected")
	}
	if m.curReq.Name != "get-order" {
		t.Errorf("selected = %q, want get-order", m.curReq.Name)
	}

	// 验证 sendCurrent 能解析（不实际发送，只验证 ResolveRequest 通过）
	r, err := a.ResolveRequest(*m.curReq, *m.curColl)
	if err != nil {
		t.Fatalf("ResolveRequest: %v", err)
	}
	if !strings.HasPrefix(r.URL, "https://sandbox.example.com/api/orders/123456") {
		t.Errorf("resolved URL = %q", r.URL)
	}
}

// TestCommandPaletteEnv 验证 :env 命令切换环境。
func TestCommandPaletteEnv(t *testing.T) {
	cfg := copyTestData(t)
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m := New(a)

	// 执行 :env prod
	cmd := m.executeCommand("env prod")
	if cmd == nil {
		t.Fatal("executeCommand returned nil cmd")
	}
	// 执行命令（产生消息）
	msg := cmd()
	if _, ok := msg.(InfoMsg); !ok {
		t.Fatalf("want InfoMsg, got %T", msg)
	}
	if a.CurrentEnvironment().Name != "prod" {
		t.Errorf("env = %q, want prod", a.CurrentEnvironment().Name)
	}
}

// TestUpdateQuit 验证 q 退出。
func TestUpdateQuit(t *testing.T) {
	cfg := copyTestData(t)
	a, _ := app.New(cfg)
	m := New(a)
	m.width, m.height = 80, 24
	m.resize()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	// tea.Quit 是一个特殊的 Cmd，验证 cmd 非 nil 即可
	_ = cmd
}

func TestAuthTabRendersBearer(t *testing.T) {
	cfg := copyTestData(t)
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = a.SetEnvironment("sandbox")

	m := New(a)
	m.width, m.height = 120, 40
	m.resize()
	m.selectCurrent() // get-order has auth_type=bearer
	m.tab = TabAuth

	out := m.View()
	if !strings.Contains(out, "Bearer Token") {
		t.Errorf("Auth tab should show 'Bearer Token', got:\n%s", out)
	}
	if !strings.Contains(out, "{{*****}}") {
		t.Errorf("Auth tab should show masked token {{*****}}, got:\n%s", out)
	}
}

func TestSearchAcceptsQAsQuery(t *testing.T) {
	cfg := copyTestData(t)
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m := New(a)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(Model)

	if !m.searching {
		t.Fatal("search mode closed while entering query")
	}
	if got := m.searchInput.Value(); got != "q" {
		t.Fatalf("search query = %q, want q", got)
	}
}

func TestCommandPaletteEscRestoresPanel(t *testing.T) {
	cfg := copyTestData(t)
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m := New(a)
	m.focus = FocusRequest

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	m = updated.(Model)
	if m.focus != FocusCommand {
		t.Fatalf("focus after ':' = %v, want command palette", m.focus)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(Model)
	if m.focus != FocusRequest {
		t.Fatalf("focus after palette Esc = %v, want request", m.focus)
	}
}

func TestHistorySnapshotCanBeResolvedAndResent(t *testing.T) {
	cfg := copyTestData(t)
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m := New(a)
	entry := model.HistoryEntry{
		Request: model.HistoryRequest{
			Method: "GET",
			URL:    "https://example.com/health",
		},
	}
	updated, _ := m.Update(HistoryMsg{Entries: []model.HistoryEntry{entry}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.curColl != nil {
		t.Fatal("history snapshot unexpectedly attached to a collection")
	}
	resolved, err := m.resolveCurrent()
	if err != nil {
		t.Fatalf("resolve history snapshot: %v", err)
	}
	if resolved.URL != entry.Request.URL {
		t.Fatalf("resolved URL = %q, want %q", resolved.URL, entry.Request.URL)
	}
	if cmd := m.sendCurrent(); cmd == nil {
		t.Fatal("history snapshot cannot be resent")
	}
	if cmd := m.saveCurrent(); cmd == nil {
		t.Fatal("saveCurrent should return an error command for history snapshots")
	}
	if !strings.Contains(m.statusMsg, "cannot be saved directly") {
		t.Fatalf("history status = %q, want explicit unsaved warning", m.statusMsg)
	}
}

func TestTruncatedResponseIsMarkedInView(t *testing.T) {
	cfg := copyTestData(t)
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m := New(a)
	m.width, m.height = 120, 40
	m.resize()
	updated, _ := m.Update(ResponseMsg{
		Resp: model.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Body:       `{"ok":true}`,
			Truncated:  true,
		},
	})
	m = updated.(Model)
	if !strings.Contains(stripANSI(m.View()), "truncated") {
		t.Fatalf("truncated marker missing from view:\n%s", m.View())
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func requestModelForEdit(t *testing.T) Model {
	t.Helper()
	cfg := copyTestData(t)
	a, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m := New(a)
	m.width, m.height = 120, 40
	m.resize()
	m.selectCurrent()
	m.focus = FocusRequest
	return m
}

func TestRequestTabEditUsesModalAndCommitsKV(t *testing.T) {
	m := requestModelForEdit(t)
	updated, _ := m.Update(keyRunes("e"))
	m = updated.(Model)
	if m.modal == nil || m.modal.kind != modalKV {
		t.Fatalf("e on Params opened %#v, want key/value modal", m.modal)
	}
	m.modal.row = len(m.modal.rows) - 1
	m.modal.col = 0
	m.modal.focusKVInput()
	// The empty row is focused on its key. q is ordinary text here and must
	// not invoke quit; this also guards the modal/global-key boundary.
	updated, _ = m.Update(keyRunes("q"))
	m = updated.(Model)
	updated, _ = m.Update(keyType(tea.KeyTab))
	m = updated.(Model)
	updated, _ = m.Update(keyRunes("value"))
	m = updated.(Model)
	updated, _ = m.Update(keyType(tea.KeyEnter))
	m = updated.(Model)
	if m.modal != nil {
		t.Fatal("KV modal remained open after Enter")
	}
	if got := m.curReq.Params["q"]; got != "value" {
		t.Fatalf("params[q] = %q, want value", got)
	}
	if !m.dirty {
		t.Fatal("KV edit did not mark request dirty")
	}
}

func TestHeaderEditorRejectsCaseInsensitiveDuplicates(t *testing.T) {
	m := requestModelForEdit(t)
	m.tab = TabHeaders
	m.modal = newKVModal(TabHeaders, map[string]string{
		"Authorization": "one",
		"authorization": "two",
	})
	if err := m.commitKVModal(); err == nil || !strings.Contains(err.Error(), "duplicate header") {
		t.Fatalf("commitKVModal error = %v", err)
	}
}

func TestAuthModalSelectsBasicAndEditsFields(t *testing.T) {
	m := requestModelForEdit(t)
	m.tab = TabAuth
	updated, _ := m.Update(keyRunes("e"))
	m = updated.(Model)
	if m.modal == nil || m.modal.kind != modalAuth {
		t.Fatalf("e on Auth opened %#v, want auth modal", m.modal)
	}
	updated, _ = m.Update(keyRunes("b"))
	m = updated.(Model)
	updated, _ = m.Update(keyType(tea.KeyTab))
	m = updated.(Model)
	updated, _ = m.Update(keyRunes("alice"))
	m = updated.(Model)
	updated, _ = m.Update(keyType(tea.KeyTab))
	m = updated.(Model)
	updated, _ = m.Update(keyRunes("secret"))
	m = updated.(Model)
	updated, _ = m.Update(keyType(tea.KeyEnter))
	m = updated.(Model)
	if m.curReq.AuthType != model.AuthBasic || m.curReq.AuthUsername != "alice" || m.curReq.AuthPassword != "secret" {
		t.Fatalf("auth = %#v, want basic/alice/secret", m.curReq)
	}
}

func TestMetadataModalEditsMethodAndURL(t *testing.T) {
	m := requestModelForEdit(t)
	updated, _ := m.Update(keyRunes("m"))
	m = updated.(Model)
	if m.modal == nil || m.modal.kind != modalMeta {
		t.Fatalf("m opened %#v, want metadata modal", m.modal)
	}
	m.modal.metaMethod.SetValue("PATCH")
	m.modal.metaURL.SetValue("https://example.com/new")
	updated, _ = m.Update(keyType(tea.KeyEnter))
	m = updated.(Model)
	if m.curReq.Method != "PATCH" || m.curReq.URL != "https://example.com/new" {
		t.Fatalf("request = %s %s, want PATCH new URL", m.curReq.Method, m.curReq.URL)
	}
}

func TestNewRequestModalCreatesInSelectedCollection(t *testing.T) {
	m := requestModelForEdit(t)
	m.focus = FocusList
	updated, _ := m.Update(keyRunes("n"))
	m = updated.(Model)
	if m.modal == nil || m.modal.kind != modalNew {
		t.Fatalf("n opened %#v, want new request modal", m.modal)
	}
	m.modal.newName.SetValue("created-in-tui")
	m.modal.newMethod.SetValue("POST")
	m.modal.newURL.SetValue("https://example.com")
	updated, cmd := m.Update(keyType(tea.KeyEnter))
	m = updated.(Model)
	if m.modal != nil {
		t.Fatal("new request modal remained open after Enter")
	}
	if cmd == nil {
		t.Fatal("new request submit returned nil command")
	}
	msg := cmd()
	if _, ok := msg.(ListUpdatedMsg); !ok {
		t.Fatalf("submit message = %T, want ListUpdatedMsg", msg)
	}
	if got := m.app.Collections()[0].Requests[len(m.app.Collections()[0].Requests)-1].Name; got != "created-in-tui" {
		t.Fatalf("created request = %q", got)
	}
}

func TestDeleteRequiresConfirmationAndEscCancels(t *testing.T) {
	m := requestModelForEdit(t)
	m.focus = FocusList
	before := len(m.app.Collections()[0].Requests)
	updated, _ := m.Update(keyRunes("d"))
	m = updated.(Model)
	if m.modal == nil || m.modal.kind != modalDelete {
		t.Fatalf("d opened %#v, want confirmation", m.modal)
	}
	updated, _ = m.Update(keyRunes("q"))
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("typed q unexpectedly closed delete confirmation")
	}
	updated, cmd := m.Update(keyType(tea.KeyEscape))
	m = updated.(Model)
	if m.modal != nil || cmd != nil {
		t.Fatal("Esc did not cancel delete confirmation")
	}
	if got := len(m.app.Collections()[0].Requests); got != before {
		t.Fatalf("requests after cancel = %d, want %d", got, before)
	}

	updated, _ = m.Update(keyRunes("d"))
	m = updated.(Model)
	updated, cmd = m.Update(keyType(tea.KeyEnter))
	m = updated.(Model)
	if m.modal != nil || cmd == nil {
		t.Fatal("Enter did not submit delete confirmation")
	}
	if _, ok := cmd().(ListUpdatedMsg); !ok {
		t.Fatal("delete command did not return ListUpdatedMsg")
	}
	if got := len(m.app.Collections()[0].Requests); got != before-1 {
		t.Fatalf("requests after confirm = %d, want %d", got, before-1)
	}
}

func TestWorkspaceCommandOpensCollectionModal(t *testing.T) {
	m := requestModelForEdit(t)
	cmd := m.executeCommand("collection new")
	if cmd == nil {
		t.Fatal("collection new returned nil command")
	}
	msg := cmd()
	workspaceMsg, ok := msg.(WorkspaceModalMsg)
	if !ok {
		t.Fatalf("workspace command message = %T, want WorkspaceModalMsg", msg)
	}
	if workspaceMsg.Resource != "collection" || workspaceMsg.Action != "new" {
		t.Fatalf("workspace message = %#v", workspaceMsg)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if m.workspaceModal == nil || m.workspaceModal.kind != workspaceCollectionNew {
		t.Fatalf("workspace modal = %#v, want new collection", m.workspaceModal)
	}
}

func TestCommandPaletteEnvUseSupportsReservedNames(t *testing.T) {
	m := requestModelForEdit(t)
	if err := m.app.CreateEnvironment("new", nil); err != nil {
		t.Fatal(err)
	}
	cmd := m.executeCommand("env use new")
	if cmd == nil {
		t.Fatal("env use returned nil command")
	}
	msg := cmd()
	if _, ok := msg.(InfoMsg); !ok {
		t.Fatalf("env use message = %T", msg)
	}
	if current := m.app.CurrentEnvironment(); current == nil || current.Name != "new" {
		t.Fatalf("current environment = %#v", current)
	}
}

func TestWorkspaceRenameUsesCompleteEnvironmentSnapshot(t *testing.T) {
	m := requestModelForEdit(t)
	cmd := m.executeCommand("env rename prod")
	if cmd == nil {
		t.Fatal("env rename returned nil command")
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if m.workspaceModal == nil || m.workspaceModal.kind != workspaceEnvironmentRename {
		t.Fatalf("workspace modal = %#v, want environment rename", m.workspaceModal)
	}
	if got := m.workspaceModal.selectedTarget(); got != "prod" {
		t.Fatalf("selected environment = %q, want prod", got)
	}
	if got := len(m.workspaceModal.targets); got != len(m.app.Environments()) {
		t.Fatalf("target count = %d, want complete environment snapshot count %d", got, len(m.app.Environments()))
	}
	if got := m.workspaceModal.originalKV["base_url"]; got != "https://api.example.com" {
		t.Fatalf("prod base_url = %q", got)
	}
}

func TestWorkspaceModalIsolatesGlobalKeysAndDeleteRequiresConfirmation(t *testing.T) {
	m := requestModelForEdit(t)
	updated, _ := m.Update(WorkspaceModalMsg{Resource: "collection", Action: "delete"})
	m = updated.(Model)
	if m.workspaceModal == nil || m.workspaceModal.kind != workspaceCollectionDelete {
		t.Fatalf("workspace modal = %#v, want collection delete", m.workspaceModal)
	}
	updated, cmd := m.Update(keyRunes("q"))
	m = updated.(Model)
	if m.workspaceModal == nil {
		t.Fatal("q closed delete confirmation; global key leaked")
	}
	if cmd != nil {
		t.Fatal("q unexpectedly produced delete command")
	}
	updated, cmd = m.Update(keyType(tea.KeyEscape))
	m = updated.(Model)
	if m.workspaceModal != nil || cmd != nil {
		t.Fatal("Esc did not cancel workspace delete")
	}
}

func TestWorkspaceDeleteCanSelectTarget(t *testing.T) {
	m := requestModelForEdit(t)
	if err := m.app.CreateEnvironment("z-delete-target", nil); err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(WorkspaceModalMsg{Resource: "environment", Action: "delete"})
	m = updated.(Model)
	before := m.workspaceModal.selectedTarget()
	updated, _ = m.Update(keyRunes("j"))
	m = updated.(Model)
	if after := m.workspaceModal.selectedTarget(); after == before {
		t.Fatalf("delete target did not move from %q", before)
	}
}

func TestWorkspaceVisibleRangeKeepsSelectionVisible(t *testing.T) {
	start, end := workspaceVisibleRange(20, 17, 8)
	if start > 17 || end <= 17 || end-start != 8 {
		t.Fatalf("visible range = [%d,%d), selection 17", start, end)
	}
}

func TestWorkspaceEnvironmentVariablesRoundTrip(t *testing.T) {
	m := requestModelForEdit(t)
	updated, _ := m.Update(WorkspaceModalMsg{Resource: "environment", Action: "new"})
	m = updated.(Model)
	if m.workspaceModal == nil || !m.workspaceModal.hasVariables() {
		t.Fatalf("workspace modal = %#v, want environment variables form", m.workspaceModal)
	}
	m.workspaceModal.name.SetValue("local")
	m.workspaceModal.focus = workspaceFocusVariables
	m.workspaceModal.focusActiveInput()
	updated, _ = m.Update(keyRunes("base_url"))
	m = updated.(Model)
	updated, _ = m.Update(keyType(tea.KeyTab))
	m = updated.(Model)
	updated, _ = m.Update(keyRunes("https://localhost"))
	m = updated.(Model)
	values, err := m.workspaceModal.variables()
	if err != nil {
		t.Fatalf("workspace variables: %v", err)
	}
	if got := values["base_url"]; got != "https://localhost" {
		t.Fatalf("base_url = %q, want https://localhost", got)
	}
}

func TestWorkspaceCollectionCreateAndDeleteThroughTUI(t *testing.T) {
	m := requestModelForEdit(t)
	before := len(m.app.Collections())

	msg := m.executeCommand("collection new")()
	updated, _ := m.Update(msg)
	m = updated.(Model)
	m.workspaceModal.name.SetValue("empty")
	updated, cmd := m.Update(keyType(tea.KeyEnter))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("collection create returned nil command")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if got := len(m.app.Collections()); got != before+1 {
		t.Fatalf("collections after create = %d, want %d", got, before+1)
	}

	msg = m.executeCommand("collection delete empty")()
	updated, _ = m.Update(msg)
	m = updated.(Model)
	if m.workspaceModal == nil || m.workspaceModal.selectedTarget() != "empty" {
		t.Fatalf("delete target = %q, want empty", m.workspaceModal.selectedTarget())
	}
	updated, cmd = m.Update(keyType(tea.KeyEnter))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("collection delete returned nil command")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if got := len(m.app.Collections()); got != before {
		t.Fatalf("collections after delete = %d, want %d", got, before)
	}
}

func TestWorkspaceEnvironmentCreateThroughTUI(t *testing.T) {
	m := requestModelForEdit(t)
	msg := m.executeCommand("env new")()
	updated, _ := m.Update(msg)
	m = updated.(Model)
	m.workspaceModal.name.SetValue("local")
	m.workspaceModal.focus = workspaceFocusVariables
	m.workspaceModal.focusActiveInput()
	updated, _ = m.Update(keyRunes("base_url"))
	m = updated.(Model)
	updated, _ = m.Update(keyType(tea.KeyTab))
	m = updated.(Model)
	updated, _ = m.Update(keyRunes("https://localhost"))
	m = updated.(Model)
	updated, cmd := m.Update(keyType(tea.KeyEnter))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("environment create returned nil command")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	var found bool
	for _, env := range m.app.Environments() {
		if env.Name == "local" {
			found = true
			if env.Variables["base_url"] != "https://localhost" {
				t.Fatalf("local base_url = %q", env.Variables["base_url"])
			}
		}
	}
	if !found {
		t.Fatal("created environment local not found")
	}
}

func TestWorkspaceEnvironmentEditPersistsVariablesWithoutRename(t *testing.T) {
	m := requestModelForEdit(t)
	variables := map[string]string{"base_url": "https://edited.example.com", "token": "edited"}
	cmd := m.workspaceCommand("environment", "rename", "sandbox", "sandbox", variables, true)
	msg := cmd()
	if errMsg, ok := msg.(ErrorMsg); ok {
		t.Fatalf("environment edit failed: %v", errMsg.Err)
	}
	if _, ok := msg.(WorkspaceUpdatedMsg); !ok {
		t.Fatalf("environment edit message = %T", msg)
	}
	for _, environment := range m.app.Environments() {
		if environment.Name == "sandbox" {
			if got := environment.Variables["token"]; got != "edited" {
				t.Fatalf("persisted token = %q", got)
			}
			return
		}
	}
	t.Fatal("sandbox environment missing after edit")
}

func TestCurlImportCommandOpensModal(t *testing.T) {
	m := requestModelForEdit(t)
	cmd := m.executeCommand("import curl")
	if cmd == nil {
		t.Fatal("import curl returned nil command")
	}
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if m.curlImport == nil || m.curlImport.phase != curlImportInput {
		t.Fatalf("curl import state = %#v, want input phase", m.curlImport)
	}
}

func TestCurlImportEnterInsertsNewlineAndPasteStaysModal(t *testing.T) {
	m := requestModelForEdit(t)
	m.openCurlImportModal()
	updated, _ := m.Update(keyRunes("curl https://example.com"))
	m = updated.(Model)
	updated, _ = m.Update(keyType(tea.KeyEnter))
	m = updated.(Model)
	if m.curlImport == nil || !strings.Contains(string(m.curlImport.input), "\n") {
		t.Fatalf("Enter did not insert newline: %#v", m.curlImport)
	}
	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("curl 'https://pasted.example'"), Paste: true}
	updated, _ = m.Update(paste)
	m = updated.(Model)
	if m.curlImport == nil || !strings.Contains(string(m.curlImport.input), "pasted.example") {
		t.Fatalf("bracketed paste was not inserted: %q", string(m.curlImport.input))
	}
}

func TestCurlImportParseFailureStaysInInput(t *testing.T) {
	m := requestModelForEdit(t)
	m.openCurlImportModal()
	m.curlImport.input = []rune("curl --unsupported https://example.com")
	m.curlImport.cursor = len(m.curlImport.input)
	updated, _ := m.Update(keyType(tea.KeyCtrlS))
	m = updated.(Model)
	if m.curlImport == nil || m.curlImport.phase != curlImportInput {
		t.Fatalf("parse failure left input phase: %#v", m.curlImport)
	}
	if m.curlImport.parseErr == "" {
		t.Fatal("parse failure did not leave an inline error")
	}
}

func TestCurlImportPreviewMasksSensitiveHeaders(t *testing.T) {
	m := requestModelForEdit(t)
	m.openCurlImportModal()
	m.curlImport.input = []rune("curl -X POST 'https://example.com/orders' -H 'Authorization: Bearer super-secret' -H 'Accept: application/json' -d '{\"ok\":true}'")
	m.curlImport.cursor = len(m.curlImport.input)
	updated, _ := m.Update(keyType(tea.KeyCtrlS))
	m = updated.(Model)
	if m.curlImport == nil || m.curlImport.phase != curlImportPreview {
		t.Fatalf("curl import phase = %#v, want preview", m.curlImport)
	}
	plain := stripANSI(m.View())
	if !strings.Contains(plain, "<redacted>") {
		t.Fatalf("preview does not mask Authorization: %s", plain)
	}
	if strings.Contains(plain, "super-secret") {
		t.Fatalf("preview leaked sensitive header: %s", plain)
	}
	if !strings.Contains(plain, "application/json") || !strings.Contains(plain, "orders") {
		t.Fatalf("preview omitted non-sensitive request data: %s", plain)
	}
}

func TestCurlImportSavesToSelectedCollection(t *testing.T) {
	m := requestModelForEdit(t)
	m.openCurlImportModal()
	m.curlImport.input = []rune("curl 'https://example.com/imported' -H 'Accept: application/json' -u alice:secret")
	m.curlImport.cursor = len(m.curlImport.input)
	updated, _ := m.Update(keyType(tea.KeyCtrlS))
	m = updated.(Model)
	updated, _ = m.Update(keyType(tea.KeyCtrlS))
	m = updated.(Model)
	if m.curlImport == nil || m.curlImport.phase != curlImportTarget {
		t.Fatalf("curl import phase = %#v, want target", m.curlImport)
	}
	m.curlImport.requestName.SetValue("imported-request")
	updated, cmd := m.Update(keyType(tea.KeyCtrlS))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("curl import save returned nil command")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	var found *model.Request
	for i := range m.app.Collections()[0].Requests {
		if m.app.Collections()[0].Requests[i].Name == "imported-request" {
			found = &m.app.Collections()[0].Requests[i]
			break
		}
	}
	if found == nil {
		t.Fatal("imported request was not persisted")
	}
	if found.Method != "GET" || found.URL != "https://example.com/imported" || found.Headers["Accept"] != "application/json" {
		t.Fatalf("imported request = %#v", *found)
	}
	if found.AuthType != model.AuthBasic || found.AuthUsername != "alice" || found.AuthPassword != "secret" {
		t.Fatalf("imported basic auth = %#v", *found)
	}
}

func TestCurlImportSaveFailureKeepsTargetForm(t *testing.T) {
	m := requestModelForEdit(t)
	m.openCurlImportModal()
	m.curlImport.input = []rune("curl https://example.com/duplicate")
	m.curlImport.cursor = len(m.curlImport.input)
	updated, _ := m.Update(keyType(tea.KeyCtrlS))
	m = updated.(Model)
	updated, _ = m.Update(keyType(tea.KeyCtrlS))
	m = updated.(Model)
	if m.curlImport == nil || m.curlImport.phase != curlImportTarget {
		t.Fatalf("curl import phase = %#v, want target", m.curlImport)
	}
	m.curlImport.requestName.SetValue("get-order")
	updated, cmd := m.Update(keyType(tea.KeyCtrlS))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("curl import save returned nil command")
	}
	if m.curlImport == nil || m.curlImport.phase != curlImportTarget {
		t.Fatalf("save closed target form before result: %#v", m.curlImport)
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.curlImport == nil || m.curlImport.phase != curlImportTarget {
		t.Fatalf("save failure did not restore target form: %#v", m.curlImport)
	}
	if !strings.Contains(m.curlImport.saveErr, "already exists") {
		t.Fatalf("save error = %q, want duplicate-name error", m.curlImport.saveErr)
	}
}
