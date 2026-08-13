package tui

import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"go.planetmeican.com/yangguang/postkid/internal/postmanimport"
)

const maxPostmanImportBytes = 64 << 20

// importPostmanPath creates an asynchronous command for the complete file
// import. Reading and parsing a collection can involve a large export, so no
// filesystem or application mutation happens while the palette is handling
// the key event itself.
func (m Model) importPostmanPath(path string) tea.Cmd {
	path = strings.TrimSpace(path)
	appModel := m.app
	return func() tea.Msg {
		if path == "" {
			return PostmanImportSaveFailedMsg{Path: path, Err: fmt.Errorf("postman collection path cannot be empty")}
		}
		if appModel == nil {
			return PostmanImportSaveFailedMsg{Path: path, Err: fmt.Errorf("application unavailable")}
		}
		data, err := readPostmanCollectionFile(path)
		if err != nil {
			return PostmanImportSaveFailedMsg{Path: path, Err: fmt.Errorf("read Postman collection %q: %w", path, err)}
		}
		result, err := postmanimport.Parse(data)
		if err != nil {
			return PostmanImportSaveFailedMsg{Path: path, Err: fmt.Errorf("parse Postman collection %q: %w", path, err)}
		}
		collection, err := appModel.ImportCollection(result.Collection)
		if err != nil {
			return PostmanImportSaveFailedMsg{Path: path, Err: fmt.Errorf("import Postman collection %q: %w", path, err)}
		}
		if collection == nil {
			return PostmanImportSaveFailedMsg{Path: path, Err: fmt.Errorf("import Postman collection %q returned no collection", path)}
		}
		return PostmanImportSavedMsg{
			Collection: collection.Name,
			Imported:   result.Imported,
			Path:       path,
		}
	}
}

func readPostmanCollectionFile(path string) ([]byte, error) {
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
	if info.Size() > maxPostmanImportBytes {
		return nil, fmt.Errorf("file exceeds %d MiB import limit", maxPostmanImportBytes>>20)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPostmanImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPostmanImportBytes {
		return nil, fmt.Errorf("file exceeds %d MiB import limit", maxPostmanImportBytes>>20)
	}
	return data, nil
}
