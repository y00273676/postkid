// Command tpost 是一个 Terminal-native API 客户端。
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"go.planetmeican.com/yangguang/postkid/internal/app"
	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/tui"
)

const version = "0.1.0"

func main() {
	var (
		showVersion bool
		dataDir     string
	)
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.StringVar(&dataDir, "dir", "", "data directory (default ~/.tpost)")
	flag.Parse()

	if showVersion {
		fmt.Println("tpost", version)
		return
	}

	cfg, err := config.Load(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	a, err := app.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init app: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(tui.New(a), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}
}
