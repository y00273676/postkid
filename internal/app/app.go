// Package app 是 Application 层门面，编排 store / env / HTTP 与 gRPC 引擎。
// 本包不 import bubbletea，保持可被未来 CLI/CI 复用。
package app

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/env"
	"go.planetmeican.com/yangguang/postkid/internal/grpcengine"
	"go.planetmeican.com/yangguang/postkid/internal/httpengine"
	"go.planetmeican.com/yangguang/postkid/internal/model"
	"go.planetmeican.com/yangguang/postkid/internal/store"
)

var requestVariablePattern = regexp.MustCompile(`\{\{\w+\}\}`)

// App 编排配置、存储、变量替换与 HTTP 引擎。
type App struct {
	cfg    *config.Config
	engine *httpengine.Engine
	grpc   *grpcengine.Engine

	collections  []model.Collection
	environments []model.Environment
	curEnv       *model.Environment
}

// New 加载数据目录并初始化各层。
func New(cfg *config.Config) (*App, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	a := &App{cfg: cfg, engine: httpengine.New(), grpc: grpcengine.New()}

	var err error
	a.collections, err = store.LoadCollections(cfg.CollectionsDir())
	if err != nil {
		return nil, fmt.Errorf("load collections: %w", err)
	}
	a.environments, err = store.LoadEnvironments(cfg.EnvironmentsDir())
	if err != nil {
		return nil, fmt.Errorf("load environments: %w", err)
	}

	// 应用持久化的 current_env。初始化时只恢复选择，不重复写配置文件。
	if cfg.CurrentEnv != "" {
		for i := range a.environments {
			if a.environments[i].Name == cfg.CurrentEnv {
				a.curEnv = &a.environments[i]
				break
			}
		}
	}
	return a, nil
}

// Collections 返回已加载的全部 collection。
func (a *App) Collections() []model.Collection { return a.collections }

// Environments 返回已加载的全部 environment。
func (a *App) Environments() []model.Environment { return a.environments }

// CurrentEnvironment 返回当前选中的 environment，可能为 nil。
func (a *App) CurrentEnvironment() *model.Environment { return a.curEnv }

// SetEnvironment 按名切换当前 environment，并持久化到 config。
func (a *App) SetEnvironment(name string) error {
	for i := range a.environments {
		if a.environments[i].Name == name {
			previous := a.cfg.CurrentEnv
			a.cfg.CurrentEnv = name
			if err := a.cfg.Save(); err != nil {
				a.cfg.CurrentEnv = previous
				return fmt.Errorf("save current environment: %w", err)
			}
			a.curEnv = &a.environments[i]
			return nil
		}
	}
	return fmt.Errorf("environment %q not found", name)
}

// ResolveRequest 合并变量、替换 {{var}}、把 params 拼到 URL，返回引擎可消费的请求。
func (a *App) ResolveRequest(req model.Request, coll model.Collection) (model.ResolvedRequest, error) {
	if req.IsGRPC() {
		return model.ResolvedRequest{}, fmt.Errorf("request %q uses gRPC; call ResolveGRPCRequest instead", req.Name)
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if !model.IsValidMethod(method) {
		return model.ResolvedRequest{}, fmt.Errorf("unsupported HTTP method %q", req.Method)
	}

	var envVars map[string]string
	if a.curEnv != nil {
		envVars = a.curEnv.Variables
	}
	vars := env.Merge(req.Variables, coll.Variables, envVars)

	// 收集所有字段替换中未命中的变量
	var missing []string
	sub := func(s string) string {
		out, m := env.Substitute(s, vars)
		missing = append(missing, m...)
		return out
	}

	finalURL := sub(req.URL)
	finalHeaders := make(map[string]string, len(req.Headers))
	for k, v := range req.Headers {
		finalHeaders[sub(k)] = sub(v)
	}
	finalParams := make(map[string]string, len(req.Params))
	for k, v := range req.Params {
		finalParams[sub(k)] = sub(v)
	}
	finalBody := sub(req.Body)

	// 从 Auth 字段生成 Authorization header（不覆盖用户显式设置的值）。
	// HTTP header 名大小写不敏感，因此这里也必须大小写不敏感地检查。
	hasAuthorization := false
	for key := range finalHeaders {
		if strings.EqualFold(key, "Authorization") {
			hasAuthorization = true
			break
		}
	}
	authType := strings.ToLower(strings.TrimSpace(req.AuthType))
	switch authType {
	case "", model.AuthNone, model.AuthBearer, model.AuthBasic:
	default:
		return model.ResolvedRequest{}, fmt.Errorf("unsupported auth type %q", req.AuthType)
	}
	if !hasAuthorization && authType != "" && authType != model.AuthNone {
		switch authType {
		case model.AuthBearer:
			subToken := sub(req.AuthToken)
			finalHeaders["Authorization"] = "Bearer " + subToken
		case model.AuthBasic:
			subUser := sub(req.AuthUsername)
			subPass := sub(req.AuthPassword)
			auth := subUser + ":" + subPass
			encoded := base64.StdEncoding.EncodeToString([]byte(auth))
			finalHeaders["Authorization"] = "Basic " + encoded
		}
	}

	if len(missing) > 0 {
		missing = uniqueSorted(missing)
		return model.ResolvedRequest{}, fmt.Errorf("undefined variables: %s", strings.Join(missing, ", "))
	}
	if err := validateHTTPURL(finalURL); err != nil {
		return model.ResolvedRequest{}, err
	}

	finalURL = appendParams(finalURL, finalParams)

	return model.ResolvedRequest{
		Method:  method,
		URL:     finalURL,
		Headers: finalHeaders,
		Body:    finalBody,
	}, nil
}

// ValidateRequest verifies a request definition before it is persisted.
// Template variables are accepted in URLs; their resolved value is validated
// again by ResolveRequest before any network call.
func ValidateRequest(req model.Request) error {
	_, err := normalizeRequest(req)
	return err
}

func normalizeRequest(req model.Request) (model.Request, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return model.Request{}, fmt.Errorf("request name cannot be empty")
	}
	if strings.ContainsAny(req.Name, "/\\") {
		return model.Request{}, fmt.Errorf("request name %q cannot contain path separators", req.Name)
	}
	for _, r := range req.Name {
		if unicode.IsControl(r) {
			return model.Request{}, fmt.Errorf("request name %q cannot contain control characters", req.Name)
		}
	}
	if req.IsGRPC() {
		return normalizeGRPCRequest(req)
	}

	req.Method = strings.ToUpper(strings.TrimSpace(req.Method))
	if !model.IsValidMethod(req.Method) {
		return model.Request{}, fmt.Errorf("unsupported HTTP method %q", req.Method)
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		return model.Request{}, fmt.Errorf("URL cannot be empty")
	}
	validationURL := req.URL
	if strings.HasPrefix(req.URL, "{{") {
		end := strings.Index(validationURL, "}}")
		if end < 0 {
			return model.Request{}, fmt.Errorf("invalid request URL %q: unterminated variable", req.URL)
		}
		validationURL = "https://example.invalid" + validationURL[end+2:]
	}
	validationURL = requestVariablePattern.ReplaceAllString(validationURL, "placeholder")
	if err := validateHTTPURL(validationURL); err != nil {
		return model.Request{}, err
	}

	authType := strings.ToLower(strings.TrimSpace(req.AuthType))
	if authType == "" {
		authType = model.AuthNone
	}
	switch authType {
	case model.AuthNone, model.AuthBasic, model.AuthBearer:
	default:
		return model.Request{}, fmt.Errorf("unsupported auth type %q", req.AuthType)
	}
	if req.AuthType != "" {
		req.AuthType = authType
	}
	return req, nil
}

func cloneCollection(c model.Collection) model.Collection {
	cloned := c
	cloned.Variables = cloneStringMap(c.Variables)
	if c.Requests != nil {
		cloned.Requests = make([]model.Request, len(c.Requests))
		for i, req := range c.Requests {
			cloned.Requests[i] = cloneRequest(req)
		}
	}
	return cloned
}

func cloneRequest(req model.Request) model.Request {
	cloned := req
	cloned.Headers = cloneStringMap(req.Headers)
	cloned.Params = cloneStringMap(req.Params)
	cloned.Variables = cloneStringMap(req.Variables)
	if req.GRPC != nil {
		grpc := *req.GRPC
		grpc.Metadata = cloneStringMap(req.GRPC.Metadata)
		grpc.ProtoFiles = append([]string(nil), req.GRPC.ProtoFiles...)
		grpc.ImportPaths = append([]string(nil), req.GRPC.ImportPaths...)
		if req.GRPC.TLS != nil {
			tls := *req.GRPC.TLS
			grpc.TLS = &tls
		}
		cloned.GRPC = &grpc
	}
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func validateHTTPURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid request URL %q: %w", rawURL, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid request URL %q: expected an absolute http or https URL", rawURL)
	}
	return nil
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

// Send 同步发送已解析的请求。TUI 应将其包成 tea.Cmd 异步执行。
func (a *App) Send(r model.ResolvedRequest) model.Response {
	return a.engine.Send(r)
}

// RecordHistory 把一次请求及其响应记录到历史。
func (a *App) RecordHistory(r model.ResolvedRequest, resp model.Response) {
	hr := model.HistoryRequest{
		Method:  r.Method,
		URL:     r.URL,
		Headers: r.Headers,
		Body:    r.Body,
	}
	hs := model.HistoryResponse{}
	if resp.Err == nil {
		hs.StatusCode = resp.StatusCode
		hs.Status = resp.Status
		hs.Latency = resp.Latency.String()
		hs.Size = resp.Size
		hs.Headers = flattenHeaders(resp.Headers)
		hs.Body = resp.Body
	}
	entry := model.HistoryEntry{
		Timestamp: time.Now(),
		Request:   hr,
		Response:  hs,
	}
	_ = store.SaveHistory(a.cfg.HistoryDir(), entry)
}

// LoadHistory 返回最近的历史记录。
func (a *App) LoadHistory() ([]model.HistoryEntry, error) {
	return store.LoadHistory(a.cfg.HistoryDir())
}

// RefreshCollections reloads all collection files from disk and replaces the
// application cache only after the complete load succeeds. Callers such as a
// TUI can rebuild their detached view from Collections after this operation.
func (a *App) RefreshCollections() error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("app and config are required")
	}
	collections, err := store.LoadCollections(a.cfg.CollectionsDir())
	if err != nil {
		return fmt.Errorf("load collections: %w", err)
	}
	a.collections = collections
	return nil
}

// ReloadCollections is an alias retained for callers that describe the same
// operation as a reload rather than a refresh.
func (a *App) ReloadCollections() error { return a.RefreshCollections() }

// CreateCollection creates an empty collection in the configured collections
// directory and adds it to the application cache after persistence succeeds.
// The returned pointer is detached from the cache, matching Collections'
// value-slice API and allowing a caller to prepare its first request safely.
func (a *App) CreateCollection(name string) (*model.Collection, error) {
	if a == nil || a.cfg == nil {
		return nil, fmt.Errorf("app and config are required")
	}
	normalized, err := store.NormalizeCollectionName(name)
	if err != nil {
		return nil, err
	}
	for _, existing := range a.collections {
		existingName, normalizeErr := store.NormalizeCollectionName(existing.Name)
		if normalizeErr == nil && existingName == normalized {
			return nil, fmt.Errorf("collection %q already exists: %w", normalized, store.ErrCollectionExists)
		}
	}
	collection, err := store.CreateCollection(a.cfg.CollectionsDir(), normalized)
	if err != nil {
		return nil, err
	}
	a.collections = append(a.collections, collection)
	result := collection
	return &result, nil
}

// AddCollection is the error-only form useful to command handlers that do not
// need the newly-created model. CreateCollection remains available when the
// caller needs the canonical FilePath immediately.
func (a *App) AddCollection(name string) error {
	_, err := a.CreateCollection(name)
	return err
}

// ImportCollection validates, atomically persists, and caches a complete
// collection. The caller's collection is never mutated and none of its maps
// or slices are shared with the returned value or the application cache.
// Cache mutation happens only after the store has installed the destination
// file successfully.
func (a *App) ImportCollection(collection model.Collection) (*model.Collection, error) {
	if a == nil || a.cfg == nil {
		return nil, fmt.Errorf("app and config are required")
	}

	normalizedName, err := store.NormalizeCollectionName(collection.Name)
	if err != nil {
		return nil, err
	}

	normalized := model.Collection{
		Name:      normalizedName,
		Variables: cloneStringMap(collection.Variables),
	}
	normalized.Requests = make([]model.Request, len(collection.Requests))
	seenRequests := make(map[string]struct{}, len(collection.Requests))
	for i, request := range collection.Requests {
		normalizedRequest, err := normalizeRequest(request)
		if err != nil {
			return nil, fmt.Errorf("request %d: %w", i+1, err)
		}
		if _, exists := seenRequests[normalizedRequest.Name]; exists {
			return nil, fmt.Errorf("duplicate request name %q", normalizedRequest.Name)
		}
		seenRequests[normalizedRequest.Name] = struct{}{}
		normalized.Requests[i] = cloneRequest(normalizedRequest)
	}

	for _, existing := range a.collections {
		existingName, normalizeErr := store.NormalizeCollectionName(existing.Name)
		if normalizeErr == nil && existingName == normalizedName {
			return nil, fmt.Errorf("collection %q already exists: %w", normalizedName, store.ErrCollectionExists)
		}
	}

	persisted, err := store.CreateCollectionWithData(a.cfg.CollectionsDir(), normalized)
	if err != nil {
		return nil, err
	}
	// Keep the cache and returned pointer detached from each other. This is
	// especially important for imported request maps, which are commonly
	// edited immediately by a TUI form after import.
	cached := cloneCollection(persisted)
	a.collections = append(a.collections, cached)
	result := cloneCollection(cached)
	return &result, nil
}

// RenameCollection changes both a collection's YAML Name and its filename.
// The store performs the filesystem transaction; the app cache is changed
// only after it reports success. Detached collections are supported as long as
// their path belongs to this app's collections directory.
func (a *App) RenameCollection(collection *model.Collection, newName string) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("app and config are required")
	}
	if collection == nil {
		return fmt.Errorf("collection is required")
	}
	if err := a.validateCollectionPath(collection.FilePath); err != nil {
		return err
	}
	normalized, err := store.NormalizeCollectionName(newName)
	if err != nil {
		return err
	}
	for _, existing := range a.collections {
		if existing.FilePath == collection.FilePath {
			continue
		}
		existingName, normalizeErr := store.NormalizeCollectionName(existing.Name)
		if normalizeErr == nil && existingName == normalized {
			return fmt.Errorf("collection %q already exists: %w", normalized, store.ErrCollectionExists)
		}
	}
	oldPath, oldName := collection.FilePath, collection.Name
	if err := store.RenameCollection(collection, normalized); err != nil {
		return err
	}
	a.replaceCachedCollection(collection, oldPath, oldName)
	return nil
}

// RenameCollectionByName is a name-oriented convenience wrapper for CLI and
// command-palette callers. The pointer-oriented method above is useful to a
// TUI that already has the selected collection model.
func (a *App) RenameCollectionByName(name, newName string) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	for i := range a.collections {
		if a.collections[i].Name == strings.TrimSpace(name) {
			return a.RenameCollection(&a.collections[i], newName)
		}
	}
	return fmt.Errorf("collection %q not found", name)
}

// DeleteCollection removes a collection YAML file and removes the successful
// target from the application cache. Deletion is intentionally explicit: the
// caller has already obtained user confirmation at the UI layer.
func (a *App) DeleteCollection(collection *model.Collection) error {
	if a == nil || a.cfg == nil {
		return fmt.Errorf("app and config are required")
	}
	if collection == nil {
		return fmt.Errorf("collection is required")
	}
	if err := a.validateCollectionPath(collection.FilePath); err != nil {
		return err
	}
	oldPath, oldName := collection.FilePath, collection.Name
	if err := store.DeleteCollection(collection); err != nil {
		return err
	}
	for i := range a.collections {
		if a.collections[i].FilePath == oldPath ||
			(a.collections[i].FilePath == "" && a.collections[i].Name == oldName) {
			a.collections = append(a.collections[:i], a.collections[i+1:]...)
			break
		}
	}
	return nil
}

// DeleteCollectionByName is the name-oriented counterpart to
// DeleteCollection. It performs no filesystem operation when the name is not
// present in the application cache.
func (a *App) DeleteCollectionByName(name string) error {
	if a == nil {
		return fmt.Errorf("app is required")
	}
	name = strings.TrimSpace(name)
	for i := range a.collections {
		if a.collections[i].Name == name {
			return a.DeleteCollection(&a.collections[i])
		}
	}
	return fmt.Errorf("collection %q not found", name)
}

// flattenHeaders 把 map[string][]string 转成 map[string]string（取第一个值）。
func flattenHeaders(h map[string][]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// SaveRequest 把单个请求回写到其所属 collection 文件。
func (a *App) SaveRequest(coll *model.Collection, req *model.Request) error {
	if coll == nil || req == nil {
		return fmt.Errorf("collection and request are required")
	}
	normalized, err := normalizeRequest(*req)
	if err != nil {
		return err
	}
	for i := range coll.Requests {
		if coll.Requests[i].Name == normalized.Name {
			previous := coll.Requests[i]
			coll.Requests[i] = normalized
			if err := store.SaveCollection(coll); err != nil {
				coll.Requests[i] = previous
				return err
			}
			*req = normalized
			a.updateCachedCollection(coll)
			return nil
		}
	}
	return fmt.Errorf("request %q not found in collection %q", req.Name, coll.Name)
}

// AddRequest 把一个新请求追加到 collection 末尾并保存。
func (a *App) AddRequest(coll *model.Collection, req *model.Request) error {
	if coll == nil || req == nil {
		return fmt.Errorf("collection and request are required")
	}
	normalized, err := normalizeRequest(*req)
	if err != nil {
		return err
	}
	for i := range coll.Requests {
		if coll.Requests[i].Name == normalized.Name {
			return fmt.Errorf("request %q already exists in collection %q", normalized.Name, coll.Name)
		}
	}
	coll.Requests = append(coll.Requests, normalized)
	if err := store.SaveCollection(coll); err != nil {
		coll.Requests = coll.Requests[:len(coll.Requests)-1]
		return err
	}
	*req = normalized
	a.updateCachedCollection(coll)
	return nil
}

// DeleteRequest 从 collection 中删除指定名称的请求并保存。
func (a *App) DeleteRequest(coll *model.Collection, name string) error {
	if coll == nil {
		return fmt.Errorf("collection is required")
	}
	for i := range coll.Requests {
		if coll.Requests[i].Name == name {
			previous := append([]model.Request(nil), coll.Requests...)
			coll.Requests = append(coll.Requests[:i], coll.Requests[i+1:]...)
			if err := store.SaveCollection(coll); err != nil {
				coll.Requests = previous
				return err
			}
			a.updateCachedCollection(coll)
			return nil
		}
	}
	return fmt.Errorf("request %q not found in collection %q", name, coll.Name)
}

// updateCachedCollection keeps the application snapshot aligned with callers
// such as the TUI, which intentionally edit a detached collection copy.
func (a *App) updateCachedCollection(coll *model.Collection) {
	for i := range a.collections {
		if a.collections[i].FilePath == coll.FilePath ||
			(a.collections[i].FilePath == "" && a.collections[i].Name == coll.Name) {
			a.collections[i] = *coll
			return
		}
	}
}

// replaceCachedCollection handles a path-changing mutation. Looking up the
// collection only after RenameCollection has changed FilePath would otherwise
// leave the old cache entry in place.
func (a *App) replaceCachedCollection(coll *model.Collection, oldPath, oldName string) {
	for i := range a.collections {
		if a.collections[i].FilePath == oldPath ||
			(a.collections[i].FilePath == "" && a.collections[i].Name == oldName) {
			a.collections[i] = *coll
			return
		}
	}
}

// validateCollectionPath prevents the App facade from operating on an
// arbitrary YAML file supplied through a detached model. Store-level APIs are
// useful for low-level tests and migration tools, while app-level CRUD is
// confined to the configured collections directory.
func (a *App) validateCollectionPath(path string) error {
	if path == "" {
		return fmt.Errorf("collection file path is required")
	}
	dir, err := filepath.Abs(filepath.Clean(a.cfg.CollectionsDir()))
	if err != nil {
		return fmt.Errorf("resolve collections directory: %w", err)
	}
	file, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("resolve collection path: %w", err)
	}
	rel, err := filepath.Rel(dir, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		filepath.Dir(file) != dir || filepath.Ext(file) != ".yaml" {
		return fmt.Errorf("collection path %q is outside the configured collections directory", path)
	}
	return nil
}

// appendParams 把 query params 拼到 URL，已存在的 query 保留。
func appendParams(rawURL string, params map[string]string) string {
	if len(params) == 0 {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL // 无法解析则原样返回，让 engine 层报错
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
