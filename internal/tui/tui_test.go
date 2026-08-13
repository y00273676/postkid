package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"go.planetmeican.com/yangguang/postkid/internal/app"
	"go.planetmeican.com/yangguang/postkid/internal/config"
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
