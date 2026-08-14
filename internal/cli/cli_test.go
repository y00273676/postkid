package cli

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
)

func TestRunRequestUsesCurrentEnvironmentAndPrintsResponseMetadata(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orders/42" || r.URL.Query().Get("detail") != "true" {
			t.Errorf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	dir := makeDataDir(t,
		"name: demo\nrequests:\n  - name: get-order\n    method: GET\n    url: \"{{endpoint}}/v1/orders/{{order_id}}\"\n    params:\n      detail: \"true\"\n",
		fmt.Sprintf("name: sandbox\nvariables:\n  endpoint: %q\n  order_id: \"42\"\n", server.URL),
		"sandbox",
	)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-dir", dir, "demo/get-order"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Run exit code = %d, stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"request: demo/get-order",
		"method: GET",
		"status: 200 OK",
		"latency:",
		"size: 11B",
		"body:\n{\n  \"ok\": true\n}",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}

	history, err := os.ReadFile(filepath.Join(dir, "history", "history.jsonl"))
	if err != nil {
		t.Fatalf("history was not recorded: %v", err)
	}
	if !strings.Contains(string(history), "/v1/orders/42?detail=true") {
		t.Errorf("history missing resolved request URL: %s", history)
	}
}

func TestRunCollectionFileRunsEveryRequest(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	dir := makeDataDir(t, fmt.Sprintf(`name: demo
requests:
  - name: first
    method: GET
    url: %q
  - name: second
    method: POST
    url: %q
`, server.URL+"/first", server.URL+"/second"), "", "")

	collectionPath := filepath.Join(dir, "collections", "demo.yaml")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"-dir", dir, "run", collectionPath}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Run exit code = %d, stderr = %s", code, stderr.String())
	}
	if got := strings.Count(stdout.String(), "request: demo/"); got != 2 {
		t.Fatalf("request output count = %d, want 2:\n%s", got, stdout.String())
	}
}

func TestRunHTTPFailureReturnsRuntimeExitCode(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream failed"))
	}))
	defer server.Close()

	dir := makeDataDir(t, fmt.Sprintf(`name: demo
requests:
  - name: fail
    method: GET
    url: %q
`, server.URL), "", "")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-dir", dir, "demo/fail"}, &stdout, &stderr)
	if code != ExitRuntime {
		t.Fatalf("Run exit code = %d, want %d", code, ExitRuntime)
	}
	if !strings.Contains(stdout.String(), "status: 502 Bad Gateway") {
		t.Errorf("status missing from output: %s", stdout.String())
	}
}

func TestRunGRPCUnaryRequestPrintsResponseAndSucceeds(t *testing.T) {
	listener, server := newGRPCTestServer(t)
	defer server.GracefulStop()
	defer listener.Close()

	dir := makeDataDir(t, fmt.Sprintf(`name: demo
requests:
  - name: unary
    protocol: grpc
    url: %q
    method: UnaryCall
    body: '{}'
    headers:
      x-cli: enabled
    grpc:
      service: grpc.testing.TestService
`, listener.Addr().String()), "", "")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-dir", dir, "demo/unary"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Run exit code = %d, stderr = %s, stdout = %s", code, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"request: demo/unary",
		"target: " + listener.Addr().String(),
		"service/method: grpc.testing.TestService/UnaryCall",
		"descriptor source: reflection",
		"grpc status: OK",
		"latency:",
		"size:",
		"body:\n",
		"payload",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestRunGRPCFailureReturnsRuntimeExitCode(t *testing.T) {
	listener, server := newGRPCTestServer(t)
	defer server.GracefulStop()
	defer listener.Close()

	dir := makeDataDir(t, fmt.Sprintf(`name: demo
requests:
  - name: unsupported
    protocol: grpc
    url: %q
    method: UnimplementedCall
    body: '{}'
    grpc:
      service: grpc.testing.TestService
`, listener.Addr().String()), "", "")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-dir", dir, "demo/unsupported"}, &stdout, &stderr)
	if code != ExitRuntime {
		t.Fatalf("Run exit code = %d, want %d; stdout = %s; stderr = %s", code, ExitRuntime, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"service/method: grpc.testing.TestService/UnimplementedCall",
		"grpc status: Unimplemented",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if !strings.Contains(stderr.String(), "request demo/unsupported") {
		t.Errorf("stderr missing request error:\n%s", stderr.String())
	}
}

func TestRunUsageErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"run"}, &stdout, &stderr); code != ExitUsage {
		t.Fatalf("missing target exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "expected exactly one") {
		t.Errorf("usage error = %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"run", "--help"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("help exit code = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("help output = %s", stdout.String())
	}
}

func TestParseArgsAcceptsOptionsBeforeAndAfterRun(t *testing.T) {
	for _, args := range [][]string{
		{"-dir", "/tmp/data", "run", "-env", "sandbox", "demo/get"},
		{"run", "--dir=/tmp/data", "--environment=sandbox", "demo/get"},
	} {
		opts, target, err := parseArgs(args)
		if err != nil {
			t.Fatalf("parseArgs(%q): %v", args, err)
		}
		if opts.dir != "/tmp/data" || opts.environment != "sandbox" || target != "demo/get" {
			t.Fatalf("parseArgs(%q) = %+v, %q", args, opts, target)
		}
	}
}

func makeDataDir(t *testing.T, collection, environment, currentEnvironment string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "collections"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "environments"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "collections", "demo.yaml"), []byte(collection), 0o644); err != nil {
		t.Fatal(err)
	}
	if environment != "" {
		if err := os.WriteFile(filepath.Join(dir, "environments", "sandbox.yaml"), []byte(environment), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if currentEnvironment != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("current_env: "+currentEnvironment+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type testServer struct {
	URL    string
	server *http.Server
}

type grpcCLITestService struct {
	grpc_testing.UnimplementedTestServiceServer
}

func (grpcCLITestService) UnaryCall(ctx context.Context, _ *grpc_testing.SimpleRequest) (*grpc_testing.SimpleResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if got := md.Get("x-cli"); len(got) != 1 || got[0] != "enabled" {
		return nil, fmt.Errorf("metadata x-cli = %v, want [enabled]", got)
	}
	if err := grpc.SetHeader(ctx, metadata.Pairs("x-response", "ok")); err != nil {
		return nil, err
	}
	grpc.SetTrailer(ctx, metadata.Pairs("x-trailer", "done"))
	return &grpc_testing.SimpleResponse{
		Payload: &grpc_testing.Payload{
			Type: grpc_testing.PayloadType_COMPRESSABLE,
			Body: []byte("hello from grpc"),
		},
	}, nil
}

func newGRPCTestServer(t *testing.T) (net.Listener, *grpc.Server) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local TCP listener unavailable: %v", err)
	}
	server := grpc.NewServer()
	grpc_testing.RegisterTestServiceServer(server, grpcCLITestService{})
	reflection.Register(server)
	go func() { _ = server.Serve(listener) }()
	return listener, server
}

func (s *testServer) Close() { _ = s.server.Close() }

func newTestServer(t *testing.T, handler http.Handler) *testServer {
	t.Helper()
	// The sandbox used by the test runner may disallow binding an IPv6
	// loopback listener, while the HTTP engine itself is happy with IPv4.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local TCP listener unavailable: %v", err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	return &testServer{URL: "http://" + listener.Addr().String(), server: server}
}
