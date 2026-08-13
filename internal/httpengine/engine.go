// Package httpengine 用 net/http 执行 ResolvedRequest。
package httpengine

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

const (
	timeout      = 30 * time.Second
	maxBodyBytes = 10 * 1024 * 1024 // 10MB，防止超大响应吃满内存
)

// Engine 封装一个可复用的 http.Client。
type Engine struct {
	client *http.Client
}

// New 创建引擎，默认 30s 超时。
func New() *Engine {
	return &Engine{client: &http.Client{Timeout: timeout}}
}

// Send 同步执行请求并返回 Response。网络/解析错误填入 Response.Err，不返回 error。
func (e *Engine) Send(r model.ResolvedRequest) model.Response {
	var bodyReader io.Reader
	if r.Body != "" {
		bodyReader = strings.NewReader(r.Body)
	}
	req, err := http.NewRequest(r.Method, r.URL, bodyReader)
	if err != nil {
		return model.Response{Err: err}
	}
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := e.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return model.Response{Err: err, Latency: latency}
	}
	defer resp.Body.Close()

	// 多读一个字节用于区分“刚好达到上限”和“确实被截断”。
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return model.Response{Err: err, Latency: latency, StatusCode: resp.StatusCode}
	}
	truncated := len(raw) > maxBodyBytes
	if truncated {
		raw = raw[:maxBodyBytes]
	}

	body := string(raw)
	// JSON 响应做 pretty print
	if isJSON(resp.Header.Get("Content-Type")) {
		if pretty, ok := prettyJSON(raw); ok {
			body = pretty
		}
	}
	size := int64(len(raw))
	if resp.ContentLength >= 0 {
		size = resp.ContentLength
	} else if truncated {
		// 对 chunked/未知长度响应只能报告已确认的下界。
		size = maxBodyBytes + 1
	}

	return model.Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Latency:    latency,
		Size:       size,
		Headers:    resp.Header,
		Body:       body,
		RawBody:    raw,
		Truncated:  truncated,
	}
}

func isJSON(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "json")
}

// prettyJSON 尝试格式化 JSON；失败（非合法 JSON）时返回 ok=false，调用方保留原文本。
func prettyJSON(raw []byte) (string, bool) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", false
	}
	return string(out), true
}
