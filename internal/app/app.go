// Package app 是 Application 层门面，编排 store / env / httpengine。
// 本包不 import bubbletea，保持可被未来 CLI/CI 复用。
package app

import (
	"fmt"
	"net/url"
	"strings"

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

	collections []model.Collection
	environments []model.Environment
	curEnv      *model.Environment
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

	// 应用持久化的 current_env
	if cfg.CurrentEnv != "" {
		_ = a.SetEnvironment(cfg.CurrentEnv) // 找不到不算致命，留空 env
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
			a.curEnv = &a.environments[i]
			a.cfg.CurrentEnv = name
			_ = a.cfg.Save()
			return nil
		}
	}
	return fmt.Errorf("environment %q not found", name)
}

// ResolveRequest 合并变量、替换 {{var}}、把 params 拼到 URL，返回引擎可消费的请求。
func (a *App) ResolveRequest(req model.Request, coll model.Collection) (model.ResolvedRequest, error) {
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

	if len(missing) > 0 {
		return model.ResolvedRequest{}, fmt.Errorf("undefined variables: %s", strings.Join(missing, ", "))
	}

	finalURL = appendParams(finalURL, finalParams)

	return model.ResolvedRequest{
		Method:  req.Method,
		URL:     finalURL,
		Headers: finalHeaders,
		Body:    finalBody,
	}, nil
}

// Send 同步发送已解析的请求。TUI 应将其包成 tea.Cmd 异步执行。
func (a *App) Send(r model.ResolvedRequest) model.Response {
	return a.engine.Send(r)
}

// SaveRequest 把单个请求回写到其所属 collection 文件。
func (a *App) SaveRequest(coll *model.Collection, req *model.Request) error {
	for i := range coll.Requests {
		if coll.Requests[i].Name == req.Name {
			coll.Requests[i] = *req
			break
		}
	}
	return store.SaveCollection(coll)
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
