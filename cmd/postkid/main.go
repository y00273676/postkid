// Command postkid 是一个 Terminal-native API 客户端。
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"go.planetmeican.com/yangguang/postkid/internal/app"
	"go.planetmeican.com/yangguang/postkid/internal/cli"
	"go.planetmeican.com/yangguang/postkid/internal/config"
	"go.planetmeican.com/yangguang/postkid/internal/tui"
)

const version = "0.1.0"

func main() {
	// Keep the default invocation interactive, but dispatch the explicit run
	// command to the non-interactive CLI. Detect run after global flags too, so
	// both `postkid run -dir ./data demo/get` and
	// `postkid -dir ./data run demo/get` work.
	if hasRunCommand(os.Args[1:]) {
		os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
	}

	var (
		showVersion bool
		dataDir     string
	)
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.StringVar(&dataDir, "dir", "", "data directory (default ~/.postkid)")
	flag.Parse()

	if showVersion {
		fmt.Println("postkid", version)
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

func hasRunCommand(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "run" {
			return true
		}
		// Avoid treating a value literally equal to "run" as the command
		// when it follows a flag that consumes a value.
		switch arg {
		case "-dir", "--dir", "-env", "--env", "-e", "--environment":
			if i+1 < len(args) {
				i++
			}
		}
	}
	return false
}
