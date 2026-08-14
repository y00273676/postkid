package grpcengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// DescriptorConfig selects a local source of protobuf descriptors. Exactly
// one of DescriptorSet and ProtoFiles may be set. ProtoFiles are compiled
// without invoking protoc; import paths are searched in order.
//
// Paths are normally made absolute by the application layer before they reach
// the engine. The loaders also accept relative paths, resolving them against
// the current working directory for callers that use the engine directly.
type DescriptorConfig struct {
	DescriptorSet string
	ProtoFiles    []string
	ImportPaths   []string
}

// DescriptorSource is an immutable, locally loaded descriptor registry. It
// can be shared by discovery and invocation without any network access. The
// registry contains the files supplied by a descriptor set or source compile;
// well-known imports are resolved from protobuf's global registry.
type DescriptorSource struct {
	registry *protoregistry.Files
}

// Registry returns the descriptor registry owned by the source. Callers must
// treat it as read-only while using the source.
func (s *DescriptorSource) Registry() *protoregistry.Files {
	if s == nil {
		return nil
	}
	return s.registry
}

// HasDescriptors reports whether config asks for a local descriptor source.
func (config DescriptorConfig) HasDescriptors() bool {
	return strings.TrimSpace(config.DescriptorSet) != "" || len(config.ProtoFiles) != 0 || len(config.ImportPaths) != 0
}

// LoadDescriptorSource loads a descriptor set or compiles proto source files.
// It rejects ambiguous configurations and validates every input before
// returning a source.
func LoadDescriptorSource(config DescriptorConfig) (*DescriptorSource, error) {
	setPath := strings.TrimSpace(config.DescriptorSet)
	if setPath != "" && len(config.ProtoFiles) != 0 {
		return nil, errors.New("gRPC descriptor_set cannot be combined with proto_files")
	}
	if len(config.ImportPaths) != 0 && len(config.ProtoFiles) == 0 {
		return nil, errors.New("gRPC import_paths require proto_files")
	}
	if setPath != "" {
		return LoadDescriptorSet(setPath)
	}
	if len(config.ProtoFiles) != 0 {
		return LoadProtoFiles(config.ProtoFiles, config.ImportPaths)
	}
	return nil, errors.New("no local gRPC descriptor source configured")
}

// LoadDescriptorSet reads a binary google.protobuf.FileDescriptorSet and
// builds a dynamic registry. Dependencies may appear in any order and are
// resolved recursively; standard well-known types are resolved globally.
func LoadDescriptorSet(path string) (*DescriptorSource, error) {
	path, err := absoluteInputPath(path, "descriptor set")
	if err != nil {
		return nil, err
	}
	data, err := readBoundedRegularFile(path, "descriptor set")
	if err != nil {
		return nil, err
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, set); err != nil {
		return nil, fmt.Errorf("decode descriptor set %q: %w", path, err)
	}
	if len(set.File) == 0 {
		return nil, fmt.Errorf("descriptor set %q contains no files", path)
	}
	registry, err := buildRegistry(set.File)
	if err != nil {
		return nil, fmt.Errorf("build descriptor set %q: %w", path, err)
	}
	return &DescriptorSource{registry: registry}, nil
}

// LoadProtoFiles parses and links one or more .proto files using
// github.com/bufbuild/protocompile. The compiler is given a constrained
// resolver rooted at importPaths and the directories containing direct input
// files, so imports cannot escape those roots through .. path components.
func LoadProtoFiles(protoFiles, importPaths []string) (*DescriptorSource, error) {
	names, roots, err := prepareProtoInputs(protoFiles, importPaths)
	if err != nil {
		return nil, err
	}
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&safeProtoResolver{roots: roots}),
	}
	compiled, err := compiler.Compile(context.Background(), names...)
	if err != nil {
		return nil, fmt.Errorf("compile proto files: %w", err)
	}
	registry, err := buildLinkedRegistry(compiled)
	if err != nil {
		return nil, fmt.Errorf("build compiled descriptors: %w", err)
	}
	return &DescriptorSource{registry: registry}, nil
}

// ServicesFromSource lists all services from a local source without dialing
// or attempting server reflection. Results are sorted by service name.
func ServicesFromSource(source *DescriptorSource) ([]model.GRPCService, error) {
	if source == nil || source.registry == nil {
		return nil, errors.New("gRPC descriptor source is nil")
	}
	return servicesFromRegistry(source.registry), nil
}

// DiscoverLocal loads and lists services from local descriptors. It never
// opens a network connection, which is useful for servers that disable
// reflection in production.
func (e *Engine) DiscoverLocal(config DescriptorConfig) ([]model.GRPCService, error) {
	source, err := LoadDescriptorSource(config)
	if err != nil {
		return nil, err
	}
	return ServicesFromSource(source)
}

// DiscoverWithConfig uses local descriptors when configured. In the absence
// of a local source it preserves the existing reflection-based Discover
// behavior. When local descriptors are present, target, TLS, and metadata are
// intentionally ignored because discovery is entirely local.
func (e *Engine) DiscoverWithConfig(ctx context.Context, target string, tlsConfig model.GRPCTLSConfig, md map[string]string, config DescriptorConfig) ([]model.GRPCService, error) {
	if config.HasDescriptors() {
		return e.DiscoverLocal(config)
	}
	return e.Discover(ctx, target, tlsConfig, md)
}

func servicesFromRegistry(registry *protoregistry.Files) []model.GRPCService {
	if registry == nil {
		return nil
	}
	services := make([]model.GRPCService, 0)
	registry.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		for i := 0; i < file.Services().Len(); i++ {
			service := file.Services().Get(i)
			info := model.GRPCService{
				Name:    string(service.FullName()),
				Methods: make([]model.GRPCMethod, 0, service.Methods().Len()),
			}
			for j := 0; j < service.Methods().Len(); j++ {
				info.Methods = append(info.Methods, methodInfo(service, service.Methods().Get(j)))
			}
			services = append(services, info)
		}
		return true
	})
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services
}

func buildLinkedRegistry(files linker.Files) (*protoregistry.Files, error) {
	if len(files) == 0 {
		return nil, errors.New("compiler returned no descriptors")
	}
	byPath := make(map[string]protoreflect.FileDescriptor, len(files))
	for _, file := range files {
		if file == nil || strings.TrimSpace(file.Path()) == "" {
			return nil, errors.New("compiled descriptor has no file name")
		}
		if previous, exists := byPath[file.Path()]; exists && previous != file {
			return nil, fmt.Errorf("compiled descriptors contain duplicate file %q", file.Path())
		}
		byPath[file.Path()] = file
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	registry := &protoregistry.Files{}
	seen := make(map[string]bool, len(byPath))
	visiting := make(map[string]bool, len(byPath))
	var add func(protoreflect.FileDescriptor) error
	add = func(file protoreflect.FileDescriptor) error {
		if file == nil {
			return errors.New("compiled descriptor contains a nil dependency")
		}
		path := file.Path()
		if seen[path] {
			return nil
		}
		if visiting[path] {
			return fmt.Errorf("descriptor import cycle at %q", path)
		}
		visiting[path] = true
		for i := 0; i < file.Imports().Len(); i++ {
			if err := add(file.Imports().Get(i)); err != nil {
				return err
			}
		}
		if err := registry.RegisterFile(file); err != nil {
			return fmt.Errorf("register descriptor %q: %w", path, err)
		}
		delete(visiting, path)
		seen[path] = true
		return nil
	}
	for _, path := range paths {
		if err := add(byPath[path]); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func prepareProtoInputs(protoFiles, importPaths []string) ([]string, []string, error) {
	if len(protoFiles) == 0 {
		return nil, nil, errors.New("at least one proto file is required")
	}
	roots := make([]string, 0, len(importPaths)+len(protoFiles))
	for _, raw := range importPaths {
		root, err := absoluteDirectoryPath(raw, "proto import path")
		if err != nil {
			return nil, nil, err
		}
		roots = appendUniquePath(roots, root)
	}
	names := make([]string, 0, len(protoFiles))
	seenNames := make(map[string]string, len(protoFiles))
	for _, raw := range protoFiles {
		path, err := absoluteInputPath(raw, "proto file")
		if err != nil {
			return nil, nil, err
		}
		if !strings.EqualFold(filepath.Ext(path), ".proto") {
			return nil, nil, fmt.Errorf("proto file %q must have a .proto extension", path)
		}
		if _, err := readBoundedRegularFile(path, "proto file"); err != nil {
			return nil, nil, err
		}
		name := ""
		for _, root := range roots {
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				name = filepath.ToSlash(rel)
				break
			}
		}
		if name == "" {
			root := filepath.Dir(path)
			roots = appendUniquePath(roots, root)
			name = filepath.Base(path)
		}
		if name == "" || name == "." || strings.HasPrefix(name, "../") {
			return nil, nil, fmt.Errorf("invalid proto file path %q", path)
		}
		if previous, exists := seenNames[name]; exists && previous != path {
			return nil, nil, fmt.Errorf("proto files %q and %q resolve to the same import name %q", previous, path, name)
		}
		seenNames[name] = path
		names = append(names, name)
	}
	return names, roots, nil
}

type safeProtoResolver struct {
	roots []string
}

func (r *safeProtoResolver) FindFileByPath(name string) (protocompile.SearchResult, error) {
	name = filepath.Clean(filepath.FromSlash(strings.TrimSpace(name)))
	if name == "." || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return protocompile.SearchResult{}, fmt.Errorf("proto import path %q escapes configured import paths", name)
	}
	var firstErr error
	for _, root := range r.roots {
		path := filepath.Join(root, name)
		if !pathWithin(root, path) {
			return protocompile.SearchResult{}, fmt.Errorf("proto import path %q escapes configured import path %q", name, root)
		}
		file, err := openBoundedRegularFile(path, "proto import")
		if err == nil {
			return protocompile.SearchResult{Source: file}, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return protocompile.SearchResult{}, err
	}
	if firstErr == nil {
		firstErr = os.ErrNotExist
	}
	return protocompile.SearchResult{}, fmt.Errorf("resolve proto import %q: %w", name, firstErr)
}

func absoluteInputPath(raw, kind string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%s path cannot be empty", kind)
	}
	if strings.IndexByte(raw, 0) >= 0 {
		return "", fmt.Errorf("%s path contains NUL byte", kind)
	}
	path, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", fmt.Errorf("resolve %s path %q: %w", kind, raw, err)
	}
	return path, nil
}

func absoluteDirectoryPath(raw, kind string) (string, error) {
	path, err := absoluteInputPath(raw, kind)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s %q: %w", kind, path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s %q is not a directory", kind, path)
	}
	return path, nil
}

func readBoundedRegularFile(path, kind string) ([]byte, error) {
	file, err := openBoundedRegularFile(path, kind)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read %s %q: %w", kind, path, err)
	}
	return data, nil
}

const maxDescriptorInputSize = 64 << 20

func openBoundedRegularFile(path, kind string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s %q: %w", kind, path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s %q is not a regular file", kind, path)
	}
	if info.Size() > maxDescriptorInputSize {
		return nil, fmt.Errorf("%s %q is too large (%d bytes; maximum is %d)", kind, path, info.Size(), maxDescriptorInputSize)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s %q: %w", kind, path, err)
	}
	return file, nil
}

func appendUniquePath(paths []string, path string) []string {
	for _, existing := range paths {
		if existing == path {
			return paths
		}
	}
	return append(paths, path)
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
