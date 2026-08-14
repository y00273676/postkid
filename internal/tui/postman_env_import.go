package tui

import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"go.planetmeican.com/yangguang/postkid/internal/postmanenv"
)

// Keep environment imports bounded for the same reason collection imports are
// bounded: the command palette should never make a large or special file an
// accidental memory allocation.
const maxPostmanEnvironmentImportBytes = 64 << 20

// importPostmanEnvironmentPath creates an asynchronous command. Parsing and
// persistence happen together before a success message is emitted, so Update
// can leave the current TUI state untouched on every failure path.
func (m Model) importPostmanEnvironmentPath(path string) tea.Cmd {
	path = strings.TrimSpace(path)
	appModel := m.app
	return func() tea.Msg {
		if path == "" {
			return PostmanEnvironmentImportSaveFailedMsg{
				Path: path,
				Err:  fmt.Errorf("postman environment path cannot be empty"),
			}
		}
		if appModel == nil {
			return PostmanEnvironmentImportSaveFailedMsg{
				Path: path,
				Err:  fmt.Errorf("application unavailable"),
			}
		}

		data, err := readPostmanEnvironmentFile(path)
		if err != nil {
			return PostmanEnvironmentImportSaveFailedMsg{
				Path: path,
				Err:  fmt.Errorf("read Postman environment %q: %w", path, err),
			}
		}
		exported, err := parsePostmanEnvironment(data)
		if err != nil {
			return PostmanEnvironmentImportSaveFailedMsg{
				Path: path,
				Err:  fmt.Errorf("parse Postman environment %q: %w", path, err),
			}
		}

		environment, err := appModel.ImportEnvironmentAndSelect(exported.Environment)
		if err != nil {
			return PostmanEnvironmentImportSaveFailedMsg{
				Path: path,
				Err:  fmt.Errorf("import Postman environment %q: %w", path, err),
			}
		}

		return PostmanEnvironmentImportSavedMsg{
			Environment: environment.Name,
			Imported:    exported.Imported,
			Path:        path,
		}
	}
}

func readPostmanEnvironmentFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if info.Size() > maxPostmanEnvironmentImportBytes {
		return nil, fmt.Errorf("file exceeds %d MiB import limit", maxPostmanEnvironmentImportBytes>>20)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPostmanEnvironmentImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPostmanEnvironmentImportBytes {
		return nil, fmt.Errorf("file exceeds %d MiB import limit", maxPostmanEnvironmentImportBytes>>20)
	}
	return data, nil
}

func parsePostmanEnvironment(data []byte) (postmanenv.Result, error) {
	return postmanenv.Parse(data)
}
