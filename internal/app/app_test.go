package app

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/model"
	"go.planetmeican.com/yangguang/postkid/internal/store"
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
	req.Headers = map[string]string{"Authorization": "Digest explicit"}
	if _, err := a.ResolveRequest(req, coll); err == nil || !strings.Contains(err.Error(), "unsupported auth type") {
		t.Fatalf("explicit header should not hide unknown auth type: %v", err)
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

func TestValidateRequest(t *testing.T) {
	valid := []model.Request{
		{Name: "health", Method: "GET", URL: "https://example.com/health"},
		{Name: "templated", Method: "post", URL: "{{base_url}}/orders/{{order_id}}", AuthType: "BEARER"},
		{Name: "host-template", Method: "PATCH", URL: "https://{{host}}/resource"},
	}
	for _, req := range valid {
		if err := ValidateRequest(req); err != nil {
			t.Errorf("ValidateRequest(%+v): %v", req, err)
		}
	}

	invalid := []model.Request{
		{Name: "", Method: "GET", URL: "https://example.com"},
		{Name: "folder/request", Method: "GET", URL: "https://example.com"},
		{Name: "bad\nname", Method: "GET", URL: "https://example.com"},
		{Name: "method", Method: "OPTIONS", URL: "https://example.com"},
		{Name: "relative", Method: "GET", URL: "/relative"},
		{Name: "scheme", Method: "GET", URL: "ftp://example.com"},
		{Name: "template", Method: "GET", URL: "{{base_url"},
		{Name: "auth", Method: "GET", URL: "https://example.com", AuthType: "digest"},
	}
	for _, req := range invalid {
		if err := ValidateRequest(req); err == nil {
			t.Errorf("ValidateRequest(%+v) unexpectedly succeeded", req)
		}
	}
}

func TestRequestMutationsValidateAndNormalizeBeforeWriting(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	detached := a.Collections()[0]
	originalLen := len(detached.Requests)
	invalid := model.Request{Name: "invalid", Method: "GET", URL: "/relative"}
	if err := a.AddRequest(&detached, &invalid); err == nil {
		t.Fatal("AddRequest accepted an invalid URL")
	}
	if len(detached.Requests) != originalLen {
		t.Fatal("invalid AddRequest mutated the collection")
	}

	valid := model.Request{Name: " normalized ", Method: " post ", URL: " https://example.com/path ", AuthType: "BASIC"}
	if err := a.AddRequest(&detached, &valid); err != nil {
		t.Fatal(err)
	}
	if valid.Name != "normalized" || valid.Method != "POST" || valid.URL != "https://example.com/path" || valid.AuthType != model.AuthBasic {
		t.Fatalf("normalized request = %+v", valid)
	}
}

func TestResolveRequestRejectsResolvedRelativeURL(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	req := model.Request{Name: "relative", Method: "GET", URL: "/relative"}
	if _, err := a.ResolveRequest(req, model.Collection{}); err == nil || !strings.Contains(err.Error(), "invalid request URL") {
		t.Fatalf("ResolveRequest error = %v", err)
	}
}

func TestCollectionCRUDUpdatesCacheAndPersists(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initial := len(a.Collections())
	created, err := a.CreateCollection("local.yaml")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if created.Name != "local" || len(a.Collections()) != initial+1 {
		t.Fatalf("created/cache = %#v/%d", created, len(a.Collections()))
	}
	if _, err := a.CreateCollection("local"); !errors.Is(err, store.ErrCollectionExists) {
		t.Fatalf("duplicate error = %v, want ErrCollectionExists", err)
	}

	created.Requests = []model.Request{{Name: "health", Method: "GET", URL: "https://example.test/health"}}
	if err := store.SaveCollection(created); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshCollections(); err != nil {
		t.Fatalf("RefreshCollections: %v", err)
	}
	var cached *model.Collection
	for i := range a.collections {
		if a.collections[i].Name == "local" {
			cached = &a.collections[i]
			break
		}
	}
	if cached == nil || len(cached.Requests) != 1 {
		t.Fatalf("refreshed cache = %#v", cached)
	}

	if err := a.RenameCollection(cached, "renamed.yaml"); err != nil {
		t.Fatalf("RenameCollection: %v", err)
	}
	if cached.Name != "renamed" || cached.FilePath != filepath.Join(cfg.CollectionsDir(), "renamed.yaml") {
		t.Fatalf("renamed model = %#v", cached)
	}
	var renamed model.Collection
	for _, got := range a.Collections() {
		if got.Name == "renamed" {
			renamed = got
			break
		}
	}
	if renamed.Name != "renamed" || len(renamed.Requests) != 1 {
		t.Fatalf("renamed cache = %#v", renamed)
	}
	if err := a.DeleteCollectionByName("renamed"); err != nil {
		t.Fatalf("DeleteCollectionByName: %v", err)
	}
	if len(a.Collections()) != initial {
		t.Fatalf("cache length after delete = %d, want %d", len(a.Collections()), initial)
	}
	if _, err := os.Stat(filepath.Join(cfg.CollectionsDir(), "renamed.yaml")); !os.IsNotExist(err) {
		t.Fatalf("deleted collection exists: %v", err)
	}
}

func TestCollectionCRUDRejectsInvalidAndExternalModels(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "../escape", "nested/name"} {
		if _, err := a.CreateCollection(name); !errors.Is(err, store.ErrInvalidName) {
			t.Errorf("CreateCollection(%q) error = %v, want ErrInvalidName", name, err)
		}
	}
	external := &model.Collection{Name: "external", FilePath: filepath.Join(t.TempDir(), "external.yaml")}
	if err := a.DeleteCollection(external); err == nil {
		t.Fatal("DeleteCollection accepted a path outside configured directory")
	}
}

func TestCollectionRefreshIsTransactionalOnLoadFailure(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]model.Collection(nil), a.Collections()...)
	badPath := filepath.Join(cfg.CollectionsDir(), "bad.yaml")
	if err := os.WriteFile(badPath, []byte("name: [invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshCollections(); err == nil {
		t.Fatal("RefreshCollections unexpectedly succeeded for invalid YAML")
	}
	if len(a.Collections()) != len(before) || a.Collections()[0].FilePath != before[0].FilePath {
		t.Fatalf("cache changed after failed refresh: %#v", a.Collections())
	}
}

func TestImportCollectionNormalizesPersistsAndDetaches(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := len(a.Collections())
	source := model.Collection{
		Name:      " imported.yaml ",
		Variables: map[string]string{"base_url": "https://example.test"},
		Requests: []model.Request{{
			Name:         " health ",
			Method:       " post ",
			URL:          " https://example.test/health ",
			Headers:      map[string]string{"Accept": "application/json"},
			Params:       map[string]string{"verbose": "true"},
			Variables:    map[string]string{"scope": "all"},
			AuthType:     " BASIC ",
			AuthUsername: "alice",
			AuthPassword: "secret",
		}},
	}
	created, err := a.ImportCollection(source)
	if err != nil {
		t.Fatalf("ImportCollection: %v", err)
	}
	if created.Name != "imported" || created.FilePath != filepath.Join(cfg.CollectionsDir(), "imported.yaml") {
		t.Fatalf("created collection = %#v", created)
	}
	if len(a.Collections()) != before+1 || len(created.Requests) != 1 {
		t.Fatalf("cache/return count = %d/%d", len(a.Collections()), len(created.Requests))
	}
	req := created.Requests[0]
	if req.Name != "health" || req.Method != "POST" || req.URL != "https://example.test/health" || req.AuthType != model.AuthBasic {
		t.Fatalf("normalized request = %#v", req)
	}

	// Mutating either the source or the detached return value must not mutate
	// the application cache (or the persisted model on disk).
	source.Variables["base_url"] = "https://changed.example"
	source.Requests[0].Headers["Accept"] = "text/plain"
	created.Variables["base_url"] = "https://returned.example"
	created.Requests[0].Params["verbose"] = "false"
	created.Requests = append(created.Requests, model.Request{Name: "local-only"})
	var cached model.Collection
	for _, candidate := range a.Collections() {
		if candidate.Name == "imported" {
			cached = candidate
			break
		}
	}
	if cached.Name == "" || len(cached.Requests) != 1 || cached.Variables["base_url"] != "https://example.test" ||
		cached.Requests[0].Headers["Accept"] != "application/json" || cached.Requests[0].Params["verbose"] != "true" {
		t.Fatalf("cache shares imported model state: %#v", cached)
	}
	loaded, err := store.LoadCollections(cfg.CollectionsDir())
	if err != nil {
		t.Fatal(err)
	}
	var persisted model.Collection
	for _, candidate := range loaded {
		if candidate.Name == "imported" {
			persisted = candidate
			break
		}
	}
	if persisted.Name == "" || len(persisted.Requests) != 1 || persisted.Requests[0].Method != "POST" {
		t.Fatalf("persisted imported collection = %#v", persisted)
	}
}

func TestImportCollectionRejectsCollisionsAndInvalidRequests(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := len(a.Collections())
	if _, err := a.ImportCollection(model.Collection{Name: " order.yaml "}); !errors.Is(err, store.ErrCollectionExists) {
		t.Fatalf("collection collision error = %v, want ErrCollectionExists", err)
	}
	if len(a.Collections()) != before {
		t.Fatalf("cache changed after collection collision: %d vs %d", len(a.Collections()), before)
	}

	invalid := model.Collection{
		Name: "invalid-import",
		Requests: []model.Request{{
			Name:   "broken",
			Method: "GET",
			URL:    "/relative",
		}},
	}
	if _, err := a.ImportCollection(invalid); err == nil || !strings.Contains(err.Error(), "invalid request URL") {
		t.Fatalf("invalid request error = %v", err)
	}
	if len(a.Collections()) != before {
		t.Fatalf("cache changed after invalid request: %d vs %d", len(a.Collections()), before)
	}
	if _, err := os.Stat(filepath.Join(cfg.CollectionsDir(), "invalid-import.yaml")); !os.IsNotExist(err) {
		t.Fatalf("invalid import left a file: %v", err)
	}

	duplicate := model.Collection{
		Name: "duplicate-import",
		Requests: []model.Request{
			{Name: "same", Method: "GET", URL: "https://example.test/one"},
			{Name: " same ", Method: "POST", URL: "https://example.test/two"},
		},
	}
	if _, err := a.ImportCollection(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate request name") {
		t.Fatalf("duplicate request error = %v", err)
	}
	if len(a.Collections()) != before {
		t.Fatalf("cache changed after duplicate request: %d vs %d", len(a.Collections()), before)
	}
}

func TestImportCollectionKeepsCacheWhenPersistenceFails(t *testing.T) {
	cfg := setupDataDir(t)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]model.Collection(nil), a.Collections()...)
	// Keep the App initialized, but point its configured store at a directory
	// that has not been created. The atomic creator must fail without changing
	// the in-memory cache.
	cfg.Dir = filepath.Join(t.TempDir(), "missing-data-dir")
	_, err = a.ImportCollection(model.Collection{
		Name: "persist-failure",
		Requests: []model.Request{{
			Name:   "request",
			Method: "GET",
			URL:    "https://example.test",
		}},
	})
	if err == nil {
		t.Fatal("ImportCollection unexpectedly succeeded with unavailable store directory")
	}
	if len(a.Collections()) != len(before) {
		t.Fatalf("cache length changed after persistence failure: %d vs %d", len(a.Collections()), len(before))
	}
	for i := range before {
		if a.Collections()[i].Name != before[i].Name || a.Collections()[i].FilePath != before[i].FilePath {
			t.Fatalf("cache changed after persistence failure: before=%#v after=%#v", before, a.Collections())
		}
	}
}
