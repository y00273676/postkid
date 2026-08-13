package httpengine

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

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
		Method: "GET",
		URL:    srv.URL + "/api/orders?detail=true",
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
