// Package model 定义 tpost 的核心数据结构。
package model

import (
	"slices"
	"time"
)

// Auth 类型常量。
const (
	AuthNone  = "none"
	AuthBasic = "basic"
	AuthBearer = "bearer"
)

// Request 表示一个 API 请求定义（collection 中的一个条目）。
type Request struct {
	Name        string            `yaml:"name"`
	Method      string            `yaml:"method"`
	URL         string            `yaml:"url"`
	Headers     map[string]string `yaml:"headers,omitempty"`
	Params      map[string]string `yaml:"params,omitempty"`
	Body        string            `yaml:"body,omitempty"`
	Variables   map[string]string `yaml:"variables,omitempty"` // request 级变量，优先级最高
	AuthType    string            `yaml:"auth_type,omitempty"`   // "none" / "basic" / "bearer"
	AuthUsername string           `yaml:"auth_username,omitempty"`
	AuthPassword string           `yaml:"auth_password,omitempty"`
	AuthToken   string            `yaml:"auth_token,omitempty"`
}

// Collection 是一组请求的集合，对应 ~/.tpost/collections 下的一个 YAML 文件。
type Collection struct {
	Name      string            `yaml:"name"`
	Variables map[string]string `yaml:"variables,omitempty"` // collection 级变量
	Requests  []Request         `yaml:"requests"`
	FilePath  string            `yaml:"-"` // 回写用，不参与序列化
}

// Environment 是一组环境变量，对应 ~/.tpost/environments 下的一个 YAML 文件。
type Environment struct {
	Name      string            `yaml:"name"`
	Variables map[string]string `yaml:"variables"`
	FilePath  string            `yaml:"-"`
}

// ResolvedRequest 是变量已替换、params 已拼到 URL 后的请求，由 HTTP 引擎消费。
type ResolvedRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}

// Response 是一次 HTTP 请求的结果。
type Response struct {
	StatusCode int
	Status     string
	Latency    time.Duration
	Size       int64
	Headers    map[string][]string
	Body       string // JSON 时为 pretty print 后的文本，否则原样
	RawBody    []byte
	Err        error
}

// ValidMethods 是 MVP 支持的 HTTP 方法。
var ValidMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// IsValidMethod 报告 method 是否被支持。
func IsValidMethod(method string) bool {
	return slices.Contains(ValidMethods, method)
}

// HistoryEntry 是历史记录中的一条，记录一次请求及其响应。
type HistoryEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	Request   HistoryRequest  `json:"request"`
	Response  HistoryResponse `json:"response"`
}

// HistoryRequest 是历史记录中保存的请求快照。
type HistoryRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// HistoryResponse 是历史记录中保存的响应快照。
type HistoryResponse struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	Latency    string            `json:"latency"` // time.Duration 序列化为字符串
	Size       int64             `json:"size"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}
