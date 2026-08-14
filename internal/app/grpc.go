package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.planetmeican.com/yangguang/postkid/internal/env"
	"go.planetmeican.com/yangguang/postkid/internal/grpcengine"
	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// ResolveGRPCRequest expands request, collection, and environment variables
// in a gRPC request. URL is interpreted as the gRPC target; Method can be a
// service-local method when grpc.service is set or a fully-qualified
// Service/Method path.
func (a *App) ResolveGRPCRequest(req model.Request, coll model.Collection) (model.ResolvedGRPCRequest, error) {
	return a.resolveGRPCRequest(req, coll, true, true)
}

// ResolveGRPCDiscoveryRequest resolves only the fields needed to discover
// services and methods. Discovery deliberately does not require a selected
// service or method (that is what discovery is for), and it does not resolve
// the request body. Descriptor paths still go through the normal collection
// relative-path and environment substitution logic.
func (a *App) ResolveGRPCDiscoveryRequest(req model.Request, coll model.Collection) (model.ResolvedGRPCRequest, error) {
	return a.resolveGRPCRequest(req, coll, false, false)
}

func (a *App) resolveGRPCRequest(req model.Request, coll model.Collection, resolveMethod, resolveBody bool) (model.ResolvedGRPCRequest, error) {
	if !req.IsGRPC() {
		return model.ResolvedGRPCRequest{}, fmt.Errorf("request %q is not a gRPC request", req.Name)
	}
	var envVars map[string]string
	if a != nil && a.curEnv != nil {
		envVars = a.curEnv.Variables
	}
	vars := env.Merge(req.Variables, coll.Variables, envVars)
	var missing []string
	sub := func(value string) string {
		out, unresolved := env.Substitute(value, vars)
		missing = append(missing, unresolved...)
		return out
	}

	resolved := model.ResolvedGRPCRequest{Target: sub(req.URL), Metadata: make(map[string]string, len(req.Headers))}
	if resolveMethod {
		resolved.Method = sub(req.Method)
	}
	metadataKeys := make(map[string]struct{}, len(req.Headers))
	explicitMetadataKeys := make(map[string]struct{})
	if req.GRPC != nil {
		if resolveMethod {
			resolved.Service = sub(req.GRPC.Service)
			if strings.TrimSpace(req.GRPC.Method) != "" {
				resolved.Method = sub(req.GRPC.Method)
			}
		}
		for key, value := range req.GRPC.Metadata {
			resolvedKey := sub(key)
			canonicalKey, err := normalizeGRPCMetadataKey(resolvedKey)
			if err != nil {
				return model.ResolvedGRPCRequest{}, err
			}
			if _, exists := metadataKeys[canonicalKey]; exists {
				return model.ResolvedGRPCRequest{}, fmt.Errorf("duplicate gRPC metadata key %q", resolvedKey)
			}
			metadataKeys[canonicalKey] = struct{}{}
			explicitMetadataKeys[canonicalKey] = struct{}{}
			resolved.Metadata[canonicalKey] = sub(value)
		}
		if req.GRPC.TLS != nil {
			resolved.TLS = resolveGRPCTLS(*req.GRPC.TLS, sub)
		}
		var err error
		resolved.ProtoFiles, resolved.ImportPaths, resolved.DescriptorSet, err = resolveGRPCDescriptors(req.GRPC, coll, sub)
		if err != nil {
			return model.ResolvedGRPCRequest{}, err
		}
	}
	// Headers are accepted as metadata as a migration convenience. Explicit
	// grpc.metadata wins when the same key appears in both maps.
	for key, value := range req.Headers {
		resolvedKey := sub(key)
		canonicalKey, err := normalizeGRPCMetadataKey(resolvedKey)
		if err != nil {
			return model.ResolvedGRPCRequest{}, err
		}
		if _, exists := metadataKeys[canonicalKey]; exists {
			if _, explicit := explicitMetadataKeys[canonicalKey]; explicit {
				continue
			}
			return model.ResolvedGRPCRequest{}, fmt.Errorf("duplicate gRPC metadata key %q", resolvedKey)
		}
		metadataKeys[canonicalKey] = struct{}{}
		resolved.Metadata[canonicalKey] = sub(value)
	}
	if resolveBody {
		resolved.Body = sub(req.Body)
	}
	if len(missing) > 0 {
		return model.ResolvedGRPCRequest{}, fmt.Errorf("undefined variables: %s", strings.Join(uniqueSorted(missing), ", "))
	}
	if strings.TrimSpace(resolved.Target) == "" {
		return model.ResolvedGRPCRequest{}, fmt.Errorf("gRPC target cannot be empty")
	}
	if resolveMethod {
		if _, _, err := grpcengine.NormalizeMethod(resolved.Service, resolved.Method); err != nil {
			return model.ResolvedGRPCRequest{}, err
		}
	}
	return resolved, nil
}

// resolveGRPCDescriptors expands and canonicalizes descriptor paths. A path
// in a collection is intentionally relative to that collection file rather
// than the process working directory; this keeps a collection portable when
// it is run by the CLI using an explicit file path.
func resolveGRPCDescriptors(cfg *model.GRPCRequest, coll model.Collection, sub func(string) string) (protoFiles, importPaths []string, descriptorSet string, err error) {
	if cfg == nil {
		return nil, nil, "", nil
	}
	if len(cfg.ProtoFiles) > 0 && strings.TrimSpace(cfg.DescriptorSet) != "" {
		return nil, nil, "", fmt.Errorf("gRPC descriptor_set cannot be combined with proto_files")
	}
	if len(cfg.ImportPaths) > 0 && len(cfg.ProtoFiles) == 0 {
		return nil, nil, "", fmt.Errorf("gRPC import_paths require proto_files")
	}
	baseDir, err := collectionDirectory(coll)
	if err != nil {
		return nil, nil, "", err
	}
	seen := make(map[string]string, len(cfg.ProtoFiles)+len(cfg.ImportPaths)+1)
	resolve := func(raw, label string) (string, error) {
		value := strings.TrimSpace(sub(raw))
		if value == "" {
			return "", fmt.Errorf("gRPC %s cannot be empty", label)
		}
		if strings.ContainsAny(value, "\r\n") {
			return "", fmt.Errorf("gRPC %s contains a newline", label)
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(baseDir, value)
		}
		value, err = filepath.Abs(value)
		if err != nil {
			return "", fmt.Errorf("resolve gRPC %s %q: %w", label, raw, err)
		}
		value = filepath.Clean(value)
		if previous, ok := seen[value]; ok {
			return "", fmt.Errorf("duplicate gRPC descriptor path %q in %s and %s", value, previous, label)
		}
		seen[value] = label
		return value, nil
	}
	for i, raw := range cfg.ProtoFiles {
		value, resolveErr := resolve(raw, fmt.Sprintf("proto_files[%d]", i))
		if resolveErr != nil {
			return nil, nil, "", resolveErr
		}
		protoFiles = append(protoFiles, value)
	}
	for i, raw := range cfg.ImportPaths {
		value, resolveErr := resolve(raw, fmt.Sprintf("import_paths[%d]", i))
		if resolveErr != nil {
			return nil, nil, "", resolveErr
		}
		importPaths = append(importPaths, value)
	}
	if strings.TrimSpace(cfg.DescriptorSet) != "" {
		descriptorSet, err = resolve(cfg.DescriptorSet, "descriptor_set")
		if err != nil {
			return nil, nil, "", err
		}
	}
	return protoFiles, importPaths, descriptorSet, nil
}

func collectionDirectory(coll model.Collection) (string, error) {
	path := strings.TrimSpace(coll.FilePath)
	if path == "" {
		path, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve gRPC descriptor base directory: %w", err)
		}
		return filepath.Clean(path), nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve collection path %q: %w", path, err)
	}
	return filepath.Dir(abs), nil
}

// GRPCDescriptorSource returns a concise description suitable for CLI/TUI
// output. An empty local descriptor configuration means server reflection.
func GRPCDescriptorSource(req model.ResolvedGRPCRequest) string {
	if req.DescriptorSet != "" {
		return "descriptor set " + req.DescriptorSet
	}
	if len(req.ProtoFiles) > 0 {
		return "proto files " + strings.Join(req.ProtoFiles, ", ")
	}
	return "reflection"
}

// ValidateGRPCRequest validates a gRPC request definition without resolving
// variables. Template values are allowed and are validated again during
// ResolveGRPCRequest.
func ValidateGRPCRequest(req model.Request) error {
	if !req.IsGRPC() {
		return fmt.Errorf("request %q is not a gRPC request", req.Name)
	}
	if strings.TrimSpace(req.URL) == "" {
		return fmt.Errorf("gRPC target cannot be empty")
	}
	service, method := "", strings.TrimSpace(req.Method)
	if req.GRPC != nil {
		service = strings.TrimSpace(req.GRPC.Service)
		if strings.TrimSpace(req.GRPC.Method) != "" {
			method = strings.TrimSpace(req.GRPC.Method)
		}
		if len(req.GRPC.ProtoFiles) > 0 && strings.TrimSpace(req.GRPC.DescriptorSet) != "" {
			return fmt.Errorf("gRPC descriptor_set cannot be combined with proto_files")
		}
		if len(req.GRPC.ImportPaths) > 0 && len(req.GRPC.ProtoFiles) == 0 {
			return fmt.Errorf("gRPC import_paths require proto_files")
		}
	}
	if strings.Contains(req.URL, "{{") {
		// The target may be a variable, so defer its transport validation.
		if strings.ContainsAny(req.URL, "\r\n") {
			return fmt.Errorf("gRPC target contains a newline")
		}
	} else if _, _, err := grpcengine.NormalizeTarget(req.URL, model.GRPCTLSConfig{}); err != nil {
		return err
	}
	_, _, err := grpcengine.NormalizeMethod(service, method)
	return err
}

// SendGRPC executes a resolved unary gRPC request.
func (a *App) SendGRPC(req model.ResolvedGRPCRequest) model.GRPCResponse {
	if a == nil || a.grpc == nil {
		return model.GRPCResponse{Err: fmt.Errorf("gRPC engine is unavailable")}
	}
	return a.grpc.Send(req)
}

// InvokeGRPC is the context-aware counterpart to SendGRPC.
func (a *App) InvokeGRPC(ctx context.Context, req model.ResolvedGRPCRequest) model.GRPCResponse {
	if a == nil || a.grpc == nil {
		return model.GRPCResponse{Err: fmt.Errorf("gRPC engine is unavailable")}
	}
	return a.grpc.Invoke(ctx, req)
}

// DiscoverGRPC returns services and methods exposed by a reflection-enabled
// server. New callers that already have a resolved request should use
// DiscoverGRPCRequest so local descriptor sources are honored.
func (a *App) DiscoverGRPC(ctx context.Context, target string, tls model.GRPCTLSConfig, metadata map[string]string) ([]model.GRPCService, error) {
	if a == nil || a.grpc == nil {
		return nil, fmt.Errorf("gRPC engine is unavailable")
	}
	return a.grpc.Discover(ctx, target, tls, metadata)
}

// DiscoverGRPCRequest discovers services from local descriptors when present,
// otherwise it falls back to server reflection.
func (a *App) DiscoverGRPCRequest(ctx context.Context, req model.ResolvedGRPCRequest) ([]model.GRPCService, error) {
	if a == nil || a.grpc == nil {
		return nil, fmt.Errorf("gRPC engine is unavailable")
	}
	return a.grpc.DiscoverRequest(ctx, req)
}

func normalizeGRPCRequest(req model.Request) (model.Request, error) {
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	if protocol == "" {
		protocol = model.ProtocolGRPC
	}
	if protocol != model.ProtocolGRPC {
		return model.Request{}, fmt.Errorf("unsupported protocol %q", req.Protocol)
	}
	req.Protocol = protocol
	if err := ValidateGRPCRequest(req); err != nil {
		return model.Request{}, err
	}
	return req, nil
}

func resolveGRPCTLS(cfg model.GRPCTLSConfig, sub func(string) string) model.GRPCTLSConfig {
	cfg.ServerName = sub(cfg.ServerName)
	cfg.CAFile = sub(cfg.CAFile)
	cfg.CertFile = sub(cfg.CertFile)
	cfg.KeyFile = sub(cfg.KeyFile)
	return cfg
}

func normalizeGRPCMetadataKey(key string) (string, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return "", fmt.Errorf("gRPC metadata key cannot be empty")
	}
	if strings.HasPrefix(key, "grpc-") {
		return "", fmt.Errorf("gRPC metadata key %q uses the reserved grpc- prefix", key)
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", fmt.Errorf("invalid gRPC metadata key %q", key)
	}
	return key, nil
}
