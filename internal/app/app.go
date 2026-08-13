// Package app 是 Application 层门面，编排 store / env / httpengine。
// 本包不 import bubbletea，保持可被未来 CLI/CI 复用。
package app

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/env"
	"go.planetmeican.com/yangguang/postkid/internal/httpengine"
	"go.planetmeican.com/yangguang/postkid/internal/model"
	"go.planetmeican.com/yangguang/postkid/internal/store"
)

// App 编排配置、存储、变量替换与 HTTP 引擎。
type App struct {
	cfg    *config.Config
	engine *httpengine.Engine

	collections  []model.Collection
	environments []model.Environment
	curEnv       *model.Environment
}

// New 加载数据目录并初始化各层。
func New(cfg *config.Config) (*App, error) {
	a := &App{cfg: cfg, engine: httpengine.New()}

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
		default:
			return model.ResolvedRequest{}, fmt.Errorf("unsupported auth type %q", req.AuthType)
		}
	}

	if len(missing) > 0 {
		missing = uniqueSorted(missing)
		return model.ResolvedRequest{}, fmt.Errorf("undefined variables: %s", strings.Join(missing, ", "))
	}

	finalURL = appendParams(finalURL, finalParams)

	return model.ResolvedRequest{
		Method:  method,
		URL:     finalURL,
		Headers: finalHeaders,
		Body:    finalBody,
	}, nil
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
	for i := range coll.Requests {
		if coll.Requests[i].Name == req.Name {
			previous := coll.Requests[i]
			coll.Requests[i] = *req
			if err := store.SaveCollection(coll); err != nil {
				coll.Requests[i] = previous
				return err
			}
			a.updateCachedCollection(coll)
			return nil
		}
	}
	return fmt.Errorf("request %q not found in collection %q", req.Name, coll.Name)
}

// AddRequest 把一个新请求追加到 collection 末尾并保存。
func (a *App) AddRequest(coll *model.Collection, req *model.Request) error {
	for i := range coll.Requests {
		if coll.Requests[i].Name == req.Name {
			return fmt.Errorf("request %q already exists in collection %q", req.Name, coll.Name)
		}
	}
	coll.Requests = append(coll.Requests, *req)
	if err := store.SaveCollection(coll); err != nil {
		coll.Requests = coll.Requests[:len(coll.Requests)-1]
		return err
	}
	a.updateCachedCollection(coll)
	return nil
}

// DeleteRequest 从 collection 中删除指定名称的请求并保存。
func (a *App) DeleteRequest(coll *model.Collection, name string) error {
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
