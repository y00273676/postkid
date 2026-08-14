// Package model 定义 postkid 的核心数据结构。
package model

import (
	"slices"
	"strings"
	"time"
)

// Protocol identifies the wire protocol used by a request. An empty protocol
// is intentionally treated as HTTP so existing collection files remain
// unchanged.
const (
	ProtocolHTTP = "http"
	ProtocolGRPC = "grpc"
)

// Auth 类型常量。
const (
	AuthNone   = "none"
	AuthBasic  = "basic"
	AuthBearer = "bearer"
)

// Request 表示一个 API 请求定义（collection 中的一个条目）。
type Request struct {
	Name         string            `yaml:"name"`
	Protocol     string            `yaml:"protocol,omitempty"`
	Method       string            `yaml:"method"`
	URL          string            `yaml:"url"`
	Headers      map[string]string `yaml:"headers,omitempty"`
	Params       map[string]string `yaml:"params,omitempty"`
	Body         string            `yaml:"body,omitempty"`
	Variables    map[string]string `yaml:"variables,omitempty"` // request 级变量，优先级最高
	AuthType     string            `yaml:"auth_type,omitempty"` // "none" / "basic" / "bearer"
	AuthUsername string            `yaml:"auth_username,omitempty"`
	AuthPassword string            `yaml:"auth_password,omitempty"`
	AuthToken    string            `yaml:"auth_token,omitempty"`
	GRPC         *GRPCRequest      `yaml:"grpc,omitempty"`
}

// GRPCRequest contains the protocol-specific part of a gRPC request. The
// target is kept in Request.URL and the JSON payload in Request.Body so HTTP
// collections keep their existing shape. Method may be either the method name
// (when Service is set) or a fully-qualified service/method path.
//
// Metadata is sent as gRPC initial metadata. Request.Headers is also accepted
// as metadata by the application layer for convenient migration of existing
// request definitions.
type GRPCRequest struct {
	Service       string            `yaml:"service,omitempty"`
	Method        string            `yaml:"method,omitempty"`
	Metadata      map[string]string `yaml:"metadata,omitempty"`
	TLS           *GRPCTLSConfig    `yaml:"tls,omitempty"`
	ProtoFiles    []string          `yaml:"proto_files,omitempty"`
	ImportPaths   []string          `yaml:"import_paths,omitempty"`
	DescriptorSet string            `yaml:"descriptor_set,omitempty"`
}

// GRPCTLSConfig configures client-side TLS for a gRPC target. With Enabled
// false the engine uses plaintext transport. CAFile is optional (the system
// roots are used when it is empty); CertFile and KeyFile enable mutual TLS.
type GRPCTLSConfig struct {
	Enabled            bool   `yaml:"enabled,omitempty"`
	ServerName         string `yaml:"server_name,omitempty"`
	CAFile             string `yaml:"ca_file,omitempty"`
	CertFile           string `yaml:"cert_file,omitempty"`
	KeyFile            string `yaml:"key_file,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify,omitempty"`
}

// ResolvedGRPCRequest is the variable-expanded representation consumed by
// grpcengine.
type ResolvedGRPCRequest struct {
	Target        string
	Service       string
	Method        string
	Body          string
	Metadata      map[string]string
	TLS           GRPCTLSConfig
	ProtoFiles    []string
	ImportPaths   []string
	DescriptorSet string
}

// GRPCMethod describes a method returned by server reflection.
type GRPCMethod struct {
	Name            string
	FullName        string
	InputType       string
	OutputType      string
	ClientStreaming bool
	ServerStreaming bool
}

// GRPCService describes a service and its reflected methods.
type GRPCService struct {
	Name    string
	Methods []GRPCMethod
}

// GRPCResponse is the result of a unary gRPC invocation.
type GRPCResponse struct {
	StatusCode int
	Status     string
	Latency    time.Duration
	Headers    map[string][]string
	Trailers   map[string][]string
	Size       int64
	Body       string
	RawBody    []byte
	Err        error
}

// IsGRPC reports whether the request is a gRPC request. The nested grpc block
// is accepted as an implicit protocol marker to make hand-written YAML less
// verbose; protocol: grpc remains the canonical form.
func (r Request) IsGRPC() bool {
	return strings.EqualFold(strings.TrimSpace(r.Protocol), ProtocolGRPC) || r.GRPC != nil
}

// Collection 是一组请求的集合，对应 ~/.postkid/collections 下的一个 YAML 文件。
type Collection struct {
	Name      string            `yaml:"name"`
	Variables map[string]string `yaml:"variables,omitempty"` // collection 级变量
	Requests  []Request         `yaml:"requests"`
	FilePath  string            `yaml:"-"` // 回写用，不参与序列化
}

// Environment 是一组环境变量，对应 ~/.postkid/environments 下的一个 YAML 文件。
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
	Truncated  bool // 响应超过引擎读取上限时为 true
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
	Timestamp time.Time       `json:"timestamp"`
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
