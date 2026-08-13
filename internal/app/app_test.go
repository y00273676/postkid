package app

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// setupDataDir 用 testdata 内容填充一个临时数据目录，返回 Config。
func setupDataDir(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"collections", "environments"} {
		src := filepath.Join("..", "..", "testdata", sub)
		dst := filepath.Join(dir, sub)
		if err := copyDir(src, dst); err != nil {
			t.Fatalf("copy %s: %v", sub, err)
		}
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func TestResolveRequest_VariableSubstitution(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.SetEnvironment("sandbox"); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}

	coll := a.Collections()[0]
	req := coll.Requests[0] // get-order: {{base_url}}/api/orders/{{order_id}}?detail=true

	resolved, err := a.ResolveRequest(req, coll)
	if err != nil {
		t.Fatalf("ResolveRequest: %v", err)
	}
	wantURL := "https://sandbox.example.com/api/orders/123456?detail=true"
	if resolved.URL != wantURL {
		t.Errorf("URL = %q, want %q", resolved.URL, wantURL)
	}
	if resolved.Headers["Authorization"] != "Bearer dev-token-xxx" {
		t.Errorf("Authorization = %q", resolved.Headers["Authorization"])
	}
}

func TestResolveRequest_MissingVariable(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 不设 environment → base_url / token / order_id 全 missing（order_id 是 collection 级，存在）
	coll := a.Collections()[0]
	req := coll.Requests[0]
	_, err = a.ResolveRequest(req, coll)
	if err == nil {
		t.Fatal("want error for missing variables, got nil")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Errorf("error should mention base_url: %v", err)
	}
}

func TestResolveRequest_EnvironmentSwitch(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	coll := a.Collections()[0]
	req := coll.Requests[0]

	_ = a.SetEnvironment("sandbox")
	r1, _ := a.ResolveRequest(req, coll)

	_ = a.SetEnvironment("prod")
	r2, _ := a.ResolveRequest(req, coll)

	if !strings.HasPrefix(r1.URL, "https://sandbox.example.com") {
		t.Errorf("sandbox URL: %q", r1.URL)
	}
	if !strings.HasPrefix(r2.URL, "https://api.example.com") {
		t.Errorf("prod URL: %q", r2.URL)
	}
}

func TestSend_Integration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"path":"` + r.URL.Path + `"}`))
	}))
	defer srv.Close()

	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 构造一个指向 test server 的请求
	coll := a.Collections()[0]
	req := coll.Requests[0]
	req.URL = srv.URL + "/api/orders/{{order_id}}"
	req.Headers = map[string]string{"Accept": "application/json"}
	req.Params = map[string]string{"detail": "true"}

	_ = a.SetEnvironment("sandbox")
	resolved, err := a.ResolveRequest(req, coll)
	if err != nil {
		t.Fatalf("ResolveRequest: %v", err)
	}
	resp := a.Send(resolved)
	if resp.Err != nil {
		t.Fatalf("Send: %v", resp.Err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Body, "/api/orders/123456") {
		t.Errorf("body missing path: %s", resp.Body)
	}
}

func TestResolveRequest_AuthBearer(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = a.SetEnvironment("sandbox")

	coll := a.Collections()[0]
	req := coll.Requests[0] // get-order has auth_type=bearer, auth_token={{token}}

	resolved, err := a.ResolveRequest(req, coll)
	if err != nil {
		t.Fatalf("ResolveRequest: %v", err)
	}
	want := "Bearer dev-token-xxx"
	if resolved.Headers["Authorization"] != want {
		t.Errorf("Authorization = %q, want %q", resolved.Headers["Authorization"], want)
	}
}

func TestResolveRequest_AuthBearer_ExplicitHeaderOverrides(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = a.SetEnvironment("sandbox")

	coll := a.Collections()[0]
	req := coll.Requests[0]
	// 用户在 Headers 中显式设置 Authorization，应覆盖 auth 字段
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	req.Headers["Authorization"] = "Bearer explicit-token"

	resolved, err := a.ResolveRequest(req, coll)
	if err != nil {
		t.Fatalf("ResolveRequest: %v", err)
	}
	want := "Bearer explicit-token"
	if resolved.Headers["Authorization"] != want {
		t.Errorf("Authorization = %q, want %q", resolved.Headers["Authorization"], want)
	}
}

func TestResolveRequest_AuthBasic(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = a.SetEnvironment("sandbox")

	coll := a.Collections()[0]
	req := model.Request{
		Name:         "basic-test",
		Method:       "GET",
		URL:          "{{base_url}}/api/test",
		AuthType:     model.AuthBasic,
		AuthUsername: "admin",
		AuthPassword: "secret123",
	}

	resolved, err := a.ResolveRequest(req, coll)
	if err != nil {
		t.Fatalf("ResolveRequest: %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret123"))
	if resolved.Headers["Authorization"] != want {
		t.Errorf("Authorization = %q, want %q", resolved.Headers["Authorization"], want)
	}
}

func TestResolveRequest_AuthNone(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = a.SetEnvironment("sandbox")

	coll := a.Collections()[0]
	req := model.Request{
		Name:     "no-auth-test",
		Method:   "GET",
		URL:      "{{base_url}}/api/test",
		AuthType: model.AuthNone,
	}

	resolved, err := a.ResolveRequest(req, coll)
	if err != nil {
		t.Fatalf("ResolveRequest: %v", err)
	}
	if _, ok := resolved.Headers["Authorization"]; ok {
		t.Errorf("Authorization header should not be set for auth_type=none")
	}
}
