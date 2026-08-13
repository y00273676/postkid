// Package cli implements the non-interactive postkid command line interface.
//
// The CLI deliberately talks to the application facade instead of reaching
// into the HTTP engine directly.  This keeps request resolution, environment
// precedence, authentication and history behaviour identical to the TUI.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"go.planetmeican.com/yangguang/postkid/internal/app"
	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// Exit codes returned by Run.  Runtime errors (including a non-2xx HTTP
// response) use one common non-zero code so callers do not have to interpret
// arbitrary HTTP status codes as process exit statuses.  Usage errors use the
// conventional command-line code 2.
const (
	ExitOK      = 0
	ExitRuntime = 1
	ExitUsage   = 2
)

const usage = `Usage:
  postkid [global options] run [run options] <collection>/<request>
  postkid [global options] run [run options] <collection.yaml>

Run options:
  -dir string
        data directory (default ~/.postkid)
  -env string
        environment name (overrides config.yaml current_env)

The collection form runs every request in the collection. The
collection/request form runs one named request.
`

// Run executes the non-interactive run command and writes human-readable
// output to stdout and diagnostics to stderr. args may contain global options
// before the "run" token (for example, "-dir ./data run demo/get") or run
// options after it. It returns an exit code suitable for os.Exit.
func Run(args []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	options, target, err := parseArgs(args)
	if err != nil {
		if errors.Is(err, errHelp) {
			fmt.Fprint(stdout, usage)
			return ExitOK
		}
		fmt.Fprintf(stderr, "postkid run: %v\n\n%s", err, usage)
		return ExitUsage
	}

	cfg, err := config.Load(options.dir)
	if err != nil {
		fmt.Fprintf(stderr, "postkid run: load config: %v\n", err)
		return ExitRuntime
	}
	a, err := app.New(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "postkid run: initialize application: %v\n", err)
		return ExitRuntime
	}
	if options.environment != "" {
		if err := a.SetEnvironment(options.environment); err != nil {
			fmt.Fprintf(stderr, "postkid run: %v\n", err)
			return ExitRuntime
		}
	}

	collection, requests, err := resolveTarget(a, cfg, target)
	if err != nil {
		fmt.Fprintf(stderr, "postkid run: %v\n", err)
		return ExitRuntime
	}
	if len(requests) == 0 {
		fmt.Fprintf(stderr, "postkid run: collection %q has no requests\n", collection.Name)
		return ExitRuntime
	}

	failed := false
	for i := range requests {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		if runRequest(a, *collection, requests[i], stdout, stderr) {
			failed = true
		}
	}
	if failed {
		return ExitRuntime
	}
	return ExitOK
}

var errHelp = errors.New("help requested")

type options struct {
	dir         string
	environment string
}

// parseArgs accepts global flags both before and after the run token. Using a
// small parser here avoids the standard flag package stopping at the first
// positional argument and makes these equivalent:
//
//	postkid -dir ./data run demo/get
//	postkid run -dir ./data demo/get
func parseArgs(args []string) (options, string, error) {
	var (
		opts    options
		targets []string
		seenRun bool
		endOpts bool
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !seenRun && arg == "run" {
			seenRun = true
			continue
		}
		if !endOpts && arg == "--" {
			endOpts = true
			continue
		}
		if !endOpts {
			if arg == "-h" || arg == "--help" {
				return options{}, "", errHelp
			}
			name, value, hasValue := splitFlag(arg)
			switch name {
			case "-dir", "--dir":
				if !hasValue {
					if i+1 >= len(args) {
						return options{}, "", fmt.Errorf("%s requires a value", name)
					}
					i++
					value = args[i]
				}
				if value == "" {
					return options{}, "", fmt.Errorf("%s must not be empty", name)
				}
				opts.dir = value
				continue
			case "-env", "--env", "-e", "--environment":
				if !hasValue {
					if i+1 >= len(args) {
						return options{}, "", fmt.Errorf("%s requires a value", name)
					}
					i++
					value = args[i]
				}
				if value == "" {
					return options{}, "", fmt.Errorf("%s must not be empty", name)
				}
				opts.environment = value
				continue
			case "":
				// A positional argument beginning with '-' is not a valid
				// collection reference. Report it as an unknown option below.
			case "-":
				// A lone dash is not a supported collection target, but let the
				// normal positional validation produce a useful error.
			default:
				if strings.HasPrefix(arg, "-") {
					return options{}, "", fmt.Errorf("unknown option %q", arg)
				}
			}
		}
		if strings.HasPrefix(arg, "-") && !endOpts {
			return options{}, "", fmt.Errorf("unknown option %q", arg)
		}
		targets = append(targets, arg)
	}
	if !seenRun {
		return options{}, "", fmt.Errorf("missing run command")
	}
	if len(targets) != 1 {
		return options{}, "", fmt.Errorf("expected exactly one collection or collection/request target")
	}
	return opts, targets[0], nil
}

func splitFlag(arg string) (name, value string, hasValue bool) {
	if i := strings.IndexByte(arg, '='); i > 0 {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}

// resolveTarget finds either a collection/request name or a collection file.
// A file path is intentionally loaded independently from App.Collections so
// CI can run a collection outside the configured collections directory while
// still using the App environment and history services.
func resolveTarget(a *app.App, cfg *config.Config, ref string) (*model.Collection, []model.Request, error) {
	if path, ok := findCollectionFile(cfg, ref); ok {
		collection, err := loadCollection(path)
		if err != nil {
			return nil, nil, fmt.Errorf("load collection %q: %w", path, err)
		}
		return &collection, append([]model.Request(nil), collection.Requests...), nil
	}

	collections := a.Collections()
	if strings.Contains(ref, "/") {
		parts := strings.SplitN(ref, "/", 2)
		collection, err := findCollection(collections, parts[0])
		if err != nil {
			return nil, nil, err
		}
		request, err := findRequest(*collection, parts[1])
		if err != nil {
			return nil, nil, err
		}
		return collection, []model.Request{*request}, nil
	}

	collection, err := findCollection(collections, ref)
	if err != nil {
		return nil, nil, err
	}
	return collection, append([]model.Request(nil), collection.Requests...), nil
}

func findCollectionFile(cfg *config.Config, ref string) (string, bool) {
	candidates := []string{ref}
	if !filepath.IsAbs(ref) {
		candidates = append(candidates,
			filepath.Join(cfg.Dir, ref),
			filepath.Join(cfg.CollectionsDir(), ref),
		)
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func loadCollection(path string) (model.Collection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Collection{}, err
	}
	var collection model.Collection
	if err := yaml.Unmarshal(data, &collection); err != nil {
		return model.Collection{}, err
	}
	collection.FilePath = path
	if collection.Name == "" {
		return model.Collection{}, fmt.Errorf("collection name is empty")
	}
	return collection, nil
}

func findCollection(collections []model.Collection, ref string) (*model.Collection, error) {
	for i := range collections {
		if collections[i].Name == ref {
			return &collections[i], nil
		}
	}
	base := strings.TrimSuffix(filepath.Base(ref), filepath.Ext(ref))
	if base != ref {
		for i := range collections {
			if collections[i].Name == base {
				return &collections[i], nil
			}
		}
	}
	return nil, fmt.Errorf("collection %q not found", ref)
}

func findRequest(collection model.Collection, name string) (*model.Request, error) {
	for i := range collection.Requests {
		if collection.Requests[i].Name == name {
			return &collection.Requests[i], nil
		}
	}
	return nil, fmt.Errorf("request %q not found in collection %q", name, collection.Name)
}

// runRequest returns true when execution should make the process fail.
func runRequest(a *app.App, collection model.Collection, request model.Request, stdout, stderr io.Writer) bool {
	resolved, err := a.ResolveRequest(request, collection)
	if err != nil {
		fmt.Fprintf(stderr, "request %s/%s: resolve: %v\n", collection.Name, request.Name, err)
		return true
	}

	response := a.Send(resolved)
	// Keep CLI executions in history just like TUI executions, including
	// transport errors where the response has no status code yet.
	a.RecordHistory(resolved, response)

	fmt.Fprintf(stdout, "request: %s/%s\n", collection.Name, request.Name)
	fmt.Fprintf(stdout, "method: %s\n", resolved.Method)
	fmt.Fprintf(stdout, "url: %s\n", resolved.URL)
	if response.Err != nil {
		fmt.Fprintln(stdout, "status: error")
		fmt.Fprintf(stdout, "latency: %s\n", response.Latency.Round(0))
		fmt.Fprintf(stdout, "size: %s\n", humanBytes(response.Size))
		fmt.Fprintln(stdout, "body:")
		fmt.Fprintf(stderr, "request %s/%s: %v\n", collection.Name, request.Name, response.Err)
		return true
	}

	fmt.Fprintf(stdout, "status: %s\n", response.Status)
	fmt.Fprintf(stdout, "latency: %s\n", response.Latency.Round(0))
	fmt.Fprintf(stdout, "size: %s\n", humanBytes(response.Size))
	fmt.Fprintln(stdout, "body:")
	if response.Body != "" {
		fmt.Fprintln(stdout, response.Body)
	}
	if response.Truncated {
		fmt.Fprintln(stdout, "… response truncated by the 10MB read limit")
	}
	return response.StatusCode >= 400
}

func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/1024/1024)
	}
}
