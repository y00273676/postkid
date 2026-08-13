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

func TestResolveRequest_NormalizesAndValidatesMethod(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	coll := a.Collections()[0]
	req := model.Request{Name: "method", Method: " post ", URL: "https://example.com"}
	resolved, err := a.ResolveRequest(req, coll)
	if err != nil {
		t.Fatalf("ResolveRequest: %v", err)
	}
	if resolved.Method != "POST" {
		t.Fatalf("method = %q, want POST", resolved.Method)
	}

	req.Method = "OPTIONS"
	if _, err := a.ResolveRequest(req, coll); err == nil || !strings.Contains(err.Error(), "unsupported HTTP method") {
		t.Fatalf("unsupported method error = %v", err)
	}
}

func TestResolveRequest_ExplicitAuthorizationIsCaseInsensitive(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	coll := a.Collections()[0]
	req := model.Request{
		Name: "auth", Method: "GET", URL: "https://example.com",
		Headers:  map[string]string{"authorization": "Bearer explicit"},
		AuthType: model.AuthBearer, AuthToken: "generated",
	}
	resolved, err := a.ResolveRequest(req, coll)
	if err != nil {
		t.Fatalf("ResolveRequest: %v", err)
	}
	if got := resolved.Headers["authorization"]; got != "Bearer explicit" {
		t.Fatalf("authorization = %q", got)
	}
	if _, exists := resolved.Headers["Authorization"]; exists {
		t.Fatal("generated Authorization should not override differently-cased explicit header")
	}
}

func TestResolveRequest_RejectsUnknownAuthAndDeduplicatesMissingVariables(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	coll := a.Collections()[0]
	req := model.Request{Name: "auth", Method: "GET", URL: "https://example.com", AuthType: "digest"}
	if _, err := a.ResolveRequest(req, coll); err == nil || !strings.Contains(err.Error(), "unsupported auth type") {
		t.Fatalf("unknown auth error = %v", err)
	}

	req = model.Request{
		Name: "missing", Method: "GET", URL: "{{same}}/{{same}}",
		Headers: map[string]string{"X-Test": "{{other}}/{{same}}"},
	}
	_, err = a.ResolveRequest(req, coll)
	if err == nil {
		t.Fatal("expected missing variable error")
	}
	if got, want := err.Error(), "undefined variables: other, same"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestRequestMutationsRejectMissingAndDuplicateNames(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	coll := &a.collections[0]
	originalLen := len(coll.Requests)

	missing := model.Request{Name: "does-not-exist", Method: "GET", URL: "https://example.com"}
	if err := a.SaveRequest(coll, &missing); err == nil {
		t.Fatal("SaveRequest should reject a request absent from the collection")
	}
	if err := a.DeleteRequest(coll, missing.Name); err == nil {
		t.Fatal("DeleteRequest should reject a request absent from the collection")
	}
	duplicate := coll.Requests[0]
	if err := a.AddRequest(coll, &duplicate); err == nil {
		t.Fatal("AddRequest should reject a duplicate name")
	}
	if len(coll.Requests) != originalLen {
		t.Fatalf("failed mutations changed request count: got %d, want %d", len(coll.Requests), originalLen)
	}
}

func TestRequestMutationsRollbackWhenPersistenceFails(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	coll := &a.collections[0]
	originalPath := coll.FilePath
	coll.FilePath = filepath.Join(t.TempDir(), "missing", "collection.yaml")
	t.Cleanup(func() { coll.FilePath = originalPath })

	original := coll.Requests[0]
	changed := original
	changed.URL = "https://changed.example.com"
	if err := a.SaveRequest(coll, &changed); err == nil {
		t.Fatal("SaveRequest should fail for an invalid destination")
	}
	if coll.Requests[0].URL != original.URL {
		t.Fatal("SaveRequest did not roll back its in-memory mutation")
	}

	originalLen := len(coll.Requests)
	added := model.Request{Name: "added", Method: "GET", URL: "https://example.com"}
	if err := a.AddRequest(coll, &added); err == nil {
		t.Fatal("AddRequest should fail for an invalid destination")
	}
	if len(coll.Requests) != originalLen {
		t.Fatal("AddRequest did not roll back its in-memory mutation")
	}

	if err := a.DeleteRequest(coll, original.Name); err == nil {
		t.Fatal("DeleteRequest should fail for an invalid destination")
	}
	if len(coll.Requests) != originalLen || coll.Requests[0].Name != original.Name {
		t.Fatal("DeleteRequest did not roll back its in-memory mutation")
	}
}

func TestRequestMutationsSynchronizeDetachedCollection(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	detached := a.Collections()[0]
	originalLen := len(detached.Requests)
	added := model.Request{Name: "added", Method: "GET", URL: "https://example.com"}

	if err := a.AddRequest(&detached, &added); err != nil {
		t.Fatal(err)
	}
	if got := len(a.Collections()[0].Requests); got != originalLen+1 {
		t.Fatalf("cached request count after add = %d, want %d", got, originalLen+1)
	}

	added.URL = "https://changed.example.com"
	if err := a.SaveRequest(&detached, &added); err != nil {
		t.Fatal(err)
	}
	if got := a.Collections()[0].Requests[originalLen].URL; got != added.URL {
		t.Fatalf("cached request URL after save = %q, want %q", got, added.URL)
	}

	if err := a.DeleteRequest(&detached, added.Name); err != nil {
		t.Fatal(err)
	}
	if got := len(a.Collections()[0].Requests); got != originalLen {
		t.Fatalf("cached request count after delete = %d, want %d", got, originalLen)
	}
}
