package httpengine

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// echoServer 回显请求的方法、路径、头、体。
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			if _, err := r.Body.Read(body); err != nil && err.Error() != "EOF" {
				t.Logf("read body: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"method":"` + r.Method + `","path":"` + r.URL.Path + `","q":"` + r.URL.RawQuery + `","auth":"` + r.Header.Get("Authorization") + `","bodyLen":` + strconv.Itoa(len(body)) + `}`))
	}))
}

func TestSend_GET(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	eng := New()
	resp := eng.Send(model.ResolvedRequest{
		Method:  "GET",
		URL:     srv.URL + "/api/orders?detail=true",
		Headers: map[string]string{"Authorization": "Bearer xyz"},
	})
	if resp.Err != nil {
		t.Fatalf("Send: %v", resp.Err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Latency <= 0 {
		t.Error("latency not recorded")
	}
	if !strings.Contains(resp.Body, `"method": "GET"`) {
		t.Errorf("body missing method echo: %s", resp.Body)
	}
	if !strings.Contains(resp.Body, `"auth": "Bearer xyz"`) {
		t.Errorf("body missing auth echo: %s", resp.Body)
	}
	// JSON pretty print：应包含换行
	if !strings.Contains(resp.Body, "\n") {
		t.Errorf("body not pretty-printed: %s", resp.Body)
	}
}

func TestSend_POST_Body(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()

	eng := New()
	resp := eng.Send(model.ResolvedRequest{
		Method:  "POST",
		URL:     srv.URL + "/api/orders",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{"sku":"A-1","qty":2}`,
	})
	if resp.Err != nil {
		t.Fatalf("Send: %v", resp.Err)
	}
	if !strings.Contains(resp.Body, `"bodyLen": 21`) {
		t.Errorf("body length not echoed: %s", resp.Body)
	}
}

func TestSend_Error(t *testing.T) {
	eng := New()
	// 无效 URL 应返回 Err 而非 panic
	resp := eng.Send(model.ResolvedRequest{Method: "GET", URL: "http://127.0.0.1:1/nonexistent"})
	if resp.Err == nil {
		t.Fatal("want error for unreachable host, got nil")
	}
}

func TestSend_MarksOversizedResponseAsTruncated(t *testing.T) {
	payload := strings.Repeat("x", maxBodyBytes+128)
	eng := &Engine{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader(payload)),
			ContentLength: int64(len(payload)),
		}, nil
	})}}

	resp := eng.Send(model.ResolvedRequest{Method: "GET", URL: "https://example.com"})
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	if !resp.Truncated {
		t.Fatal("oversized response should be marked truncated")
	}
	if len(resp.RawBody) != maxBodyBytes {
		t.Fatalf("raw body size = %d, want %d", len(resp.RawBody), maxBodyBytes)
	}
	if resp.Size != int64(len(payload)) {
		t.Fatalf("reported size = %d, want %d", resp.Size, len(payload))
	}
	if resp.Body != string(resp.RawBody) {
		t.Fatal("engine should keep body content pure and expose truncation as metadata")
	}
}
