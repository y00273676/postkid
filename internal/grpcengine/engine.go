// Package grpcengine discovers and invokes unary gRPC methods.
//
// The engine deliberately uses the standard server-reflection protocol and
// protobuf's dynamic messages. It therefore works with services for which
// postkid has no generated Go client, while keeping the dependency surface
// smaller than a complete grpcurl integration.
package grpcengine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

const (
	defaultDialTimeout = 10 * time.Second
	defaultCallTimeout = 30 * time.Second
)

// Engine is a reusable gRPC request executor. A new connection is opened for
// each discovery/invocation; this keeps TLS and metadata options isolated and
// avoids stale connections after a local server is restarted.
type Engine struct {
	dialTimeout time.Duration
	callTimeout time.Duration
}

// New creates an engine with conservative network timeouts.
func New() *Engine {
	return &Engine{dialTimeout: defaultDialTimeout, callTimeout: defaultCallTimeout}
}

// Discover uses server reflection to list services and their methods. The
// returned methods include stream direction so callers can disable unsupported
// streaming methods before attempting invocation.
func (e *Engine) Discover(ctx context.Context, target string, tlsConfig model.GRPCTLSConfig, md map[string]string) ([]model.GRPCService, error) {
	if e == nil {
		e = New()
	}
	ctx, cancel := withTimeout(ctx, e.callTimeout)
	defer cancel()

	endpoint, tlsConfig, err := normalizeTarget(target, tlsConfig)
	if err != nil {
		return nil, err
	}
	conn, err := e.dial(ctx, endpoint, tlsConfig)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	services, err := listServices(ctx, conn, md)
	if err != nil {
		return nil, err
	}
	result := make([]model.GRPCService, 0, len(services))
	for _, service := range services {
		files, err := filesForSymbol(ctx, conn, md, service)
		if err != nil {
			return nil, fmt.Errorf("reflect service %q: %w", service, err)
		}
		registry, err := buildRegistry(files)
		if err != nil {
			return nil, fmt.Errorf("build descriptors for %q: %w", service, err)
		}
		desc, err := registry.FindDescriptorByName(protoreflect.FullName(service))
		if err != nil {
			return nil, fmt.Errorf("find service %q: %w", service, err)
		}
		svc, ok := desc.(protoreflect.ServiceDescriptor)
		if !ok {
			return nil, fmt.Errorf("reflection symbol %q is not a service", service)
		}
		info := model.GRPCService{Name: service, Methods: make([]model.GRPCMethod, 0, svc.Methods().Len())}
		for i := 0; i < svc.Methods().Len(); i++ {
			method := svc.Methods().Get(i)
			info.Methods = append(info.Methods, methodInfo(svc, method))
		}
		result = append(result, info)
	}
	return result, nil
}

// ListServices is a lighter reflection operation for callers that only need
// names. It accepts the same transport and metadata options as Discover.
func (e *Engine) ListServices(ctx context.Context, target string, tlsConfig model.GRPCTLSConfig, md map[string]string) ([]string, error) {
	if e == nil {
		e = New()
	}
	ctx, cancel := withTimeout(ctx, e.callTimeout)
	defer cancel()
	endpoint, tlsConfig, err := normalizeTarget(target, tlsConfig)
	if err != nil {
		return nil, err
	}
	conn, err := e.dial(ctx, endpoint, tlsConfig)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return listServices(ctx, conn, md)
}

// DiscoverRequest discovers services for a resolved request. If the request
// includes proto_files/import_paths or descriptor_set, discovery is entirely
// local and does not dial the request target. Otherwise it uses the existing
// server-reflection path.
func (e *Engine) DiscoverRequest(ctx context.Context, req model.ResolvedGRPCRequest) ([]model.GRPCService, error) {
	return e.DiscoverWithConfig(ctx, req.Target, req.TLS, req.Metadata, descriptorConfig(req))
}

// Send invokes one resolved unary request using a background context. It is
// the gRPC analogue of httpengine.Engine.Send.
func (e *Engine) Send(req model.ResolvedGRPCRequest) model.GRPCResponse {
	return e.Invoke(context.Background(), req)
}

// Invoke performs reflection, validates that the selected method is unary,
// unmarshals the JSON body into a dynamic protobuf message, invokes the RPC,
// and marshals the response back to pretty JSON.
func (e *Engine) Invoke(ctx context.Context, req model.ResolvedGRPCRequest) model.GRPCResponse {
	started := time.Now()
	response := model.GRPCResponse{Headers: map[string][]string{}, Trailers: map[string][]string{}}
	if e == nil {
		e = New()
	}
	ctx, cancel := withTimeout(ctx, e.callTimeout)
	defer cancel()

	endpoint, tlsConfig, err := normalizeTarget(req.Target, req.TLS)
	if err != nil {
		response.Err = err
		response.Latency = time.Since(started)
		return response
	}
	service, methodName, err := splitMethod(req.Service, req.Method)
	if err != nil {
		response.Err = err
		response.Latency = time.Since(started)
		return response
	}
	var registry *protoregistry.Files
	config := descriptorConfig(req)
	if config.HasDescriptors() {
		source, sourceErr := LoadDescriptorSource(config)
		if sourceErr != nil {
			response.Err = sourceErr
			response.Latency = time.Since(started)
			return response
		}
		registry = source.Registry()
	}
	conn, err := e.dial(ctx, endpoint, tlsConfig)
	if err != nil {
		response.Err = err
		response.Latency = time.Since(started)
		return response
	}
	defer conn.Close()

	if registry == nil {
		files, reflectErr := filesForSymbol(ctx, conn, req.Metadata, service)
		if reflectErr != nil {
			response.Err = fmt.Errorf("reflect service %q: %w", service, reflectErr)
			response.Latency = time.Since(started)
			return response
		}
		registry, err = buildRegistry(files)
		if err != nil {
			response.Err = fmt.Errorf("build descriptors for %q: %w", service, err)
			response.Latency = time.Since(started)
			return response
		}
	}
	desc, err := registry.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		response.Err = fmt.Errorf("find service %q: %w", service, err)
		response.Latency = time.Since(started)
		return response
	}
	svc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		response.Err = fmt.Errorf("descriptor symbol %q is not a service", service)
		response.Latency = time.Since(started)
		return response
	}
	method := svc.Methods().ByName(protoreflect.Name(methodName))
	if method == nil {
		response.Err = fmt.Errorf("gRPC method %q not found in service %q", methodName, service)
		response.Latency = time.Since(started)
		return response
	}
	if method.IsStreamingClient() || method.IsStreamingServer() {
		response.Err = fmt.Errorf("gRPC streaming method %q is unsupported; only unary RPCs are supported", fullMethodName(service, methodName))
		response.Latency = time.Since(started)
		return response
	}

	input := dynamicpb.NewMessage(method.Input())
	if strings.TrimSpace(req.Body) != "" {
		if err := protojson.Unmarshal([]byte(req.Body), input); err != nil {
			response.Err = fmt.Errorf("decode gRPC request JSON: %w", err)
			response.Latency = time.Since(started)
			return response
		}
	}
	output := dynamicpb.NewMessage(method.Output())
	callCtx := withOutgoingMetadata(ctx, req.Metadata)
	var headers, trailers metadata.MD
	callErr := conn.Invoke(callCtx, fullMethodName(service, methodName), input, output,
		grpc.Header(&headers), grpc.Trailer(&trailers))
	response.Headers = cloneMetadata(headers)
	response.Trailers = cloneMetadata(trailers)
	response.StatusCode = int(status.Code(callErr))
	response.Status = status.Code(callErr).String()
	if callErr != nil {
		response.Err = callErr
		response.Latency = time.Since(started)
		return response
	}
	jsonBody, err := (protojson.MarshalOptions{Indent: "  "}).Marshal(proto.Message(output))
	if err != nil {
		response.Err = fmt.Errorf("encode gRPC response JSON: %w", err)
		response.Latency = time.Since(started)
		return response
	}
	response.RawBody = jsonBody
	response.Body = string(jsonBody)
	response.Size = int64(len(jsonBody))
	response.Latency = time.Since(started)
	return response
}

func (e *Engine) dial(ctx context.Context, target string, tlsConfig model.GRPCTLSConfig) (*grpc.ClientConn, error) {
	transport, err := transportCredentials(tlsConfig, target)
	if err != nil {
		return nil, err
	}
	dialCtx, cancel := withTimeout(ctx, e.dialTimeout)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx, target, grpc.WithTransportCredentials(transport), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("dial gRPC target %q: %w", target, err)
	}
	return conn, nil
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok || timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func withOutgoingMetadata(ctx context.Context, values map[string]string) context.Context {
	if len(values) == 0 {
		return ctx
	}
	md := metadata.New(nil)
	for key, value := range values {
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "" {
			md.Set(key, value)
		}
	}
	return metadata.NewOutgoingContext(ctx, md)
}

func normalizeTarget(raw string, tlsConfig model.GRPCTLSConfig) (string, model.GRPCTLSConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", tlsConfig, errors.New("gRPC target cannot be empty")
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", tlsConfig, fmt.Errorf("invalid gRPC target %q: %w", raw, err)
		}
		switch strings.ToLower(u.Scheme) {
		case "grpc":
		case "grpcs", "https":
			tlsConfig.Enabled = true
		case "http":
		default:
			return "", tlsConfig, fmt.Errorf("unsupported gRPC target scheme %q", u.Scheme)
		}
		if u.Host == "" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
			return "", tlsConfig, fmt.Errorf("gRPC target %q must contain only a host and optional port", raw)
		}
		raw = u.Host
	}
	if strings.ContainsAny(raw, "\r\n") {
		return "", tlsConfig, errors.New("gRPC target contains a newline")
	}
	return raw, tlsConfig, nil
}

// NormalizeTarget validates and normalizes a gRPC endpoint without dialing it.
// Schemes grpc:// and grpcs:// are accepted; grpcs:// and https:// enable TLS.
func NormalizeTarget(raw string, tlsConfig model.GRPCTLSConfig) (string, model.GRPCTLSConfig, error) {
	return normalizeTarget(raw, tlsConfig)
}

func transportCredentials(cfg model.GRPCTLSConfig, target string) (credentials.TransportCredentials, error) {
	if !cfg.Enabled {
		if cfg.CAFile != "" || cfg.CertFile != "" || cfg.KeyFile != "" || cfg.ServerName != "" || cfg.InsecureSkipVerify {
			return nil, errors.New("gRPC TLS options require tls.enabled: true")
		}
		return insecure.NewCredentials(), nil
	}
	rootCAs, err := systemRoots(cfg.CAFile)
	if err != nil {
		return nil, err
	}
	serverName := strings.TrimSpace(cfg.ServerName)
	if serverName == "" {
		serverName = targetHost(target)
	}
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		RootCAs:            rootCAs,
		ServerName:         serverName,
		InsecureSkipVerify: cfg.InsecureSkipVerify, // explicitly configured for local/self-signed endpoints
	}
	if cfg.CertFile != "" || cfg.KeyFile != "" {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return nil, errors.New("gRPC TLS cert_file and key_file must be configured together")
		}
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load gRPC client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return credentials.NewTLS(tlsConfig), nil
}

func systemRoots(caFile string) (*x509.CertPool, error) {
	if caFile == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Clean(caFile))
	if err != nil {
		return nil, fmt.Errorf("read gRPC CA file %q: %w", caFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("gRPC CA file %q contains no certificates", caFile)
	}
	return pool, nil
}

func targetHost(target string) string {
	host, _, err := net.SplitHostPort(target)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(target, "[]")
}

func fullMethodName(service, method string) string {
	return "/" + strings.Trim(service, "/") + "/" + strings.Trim(method, "/")
}

func splitMethod(service, method string) (string, string, error) {
	service = strings.Trim(strings.TrimSpace(service), "/")
	method = strings.Trim(strings.TrimSpace(method), "/")
	if strings.Contains(method, "/") {
		parts := strings.Split(method, "/")
		if len(parts) != 2 || service != "" && parts[0] != service {
			return "", "", fmt.Errorf("invalid gRPC method %q; expected Service/Method", method)
		}
		service, method = parts[0], parts[1]
	}
	if service == "" || method == "" || strings.ContainsAny(service, " \t\r\n") || strings.ContainsAny(method, " \t\r\n") {
		return "", "", errors.New("gRPC service and method are required")
	}
	return service, method, nil
}

// NormalizeMethod validates a service/method pair and returns the split names.
func NormalizeMethod(service, method string) (string, string, error) {
	return splitMethod(service, method)
}

func descriptorConfig(req model.ResolvedGRPCRequest) DescriptorConfig {
	return DescriptorConfig{
		DescriptorSet: req.DescriptorSet,
		ProtoFiles:    append([]string(nil), req.ProtoFiles...),
		ImportPaths:   append([]string(nil), req.ImportPaths...),
	}
}

func methodInfo(service protoreflect.ServiceDescriptor, method protoreflect.MethodDescriptor) model.GRPCMethod {
	return model.GRPCMethod{
		Name:            string(method.Name()),
		FullName:        fullMethodName(string(service.FullName()), string(method.Name())),
		InputType:       string(method.Input().FullName()),
		OutputType:      string(method.Output().FullName()),
		ClientStreaming: method.IsStreamingClient(),
		ServerStreaming: method.IsStreamingServer(),
	}
}
