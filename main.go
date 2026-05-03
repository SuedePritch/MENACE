package main

import (
	"fmt"
	"os"
	"path/filepath"

	mlog "menace/internal/log"
	"menace/internal/store"
	"menace/internal/tui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "unleash", "run":
			// proceed
		case "help", "--help", "-h":
			fmt.Println("Usage: menace [unleash]")
			fmt.Println("")
			fmt.Println("  unleash    Start the MENACE dashboard (default)")
			fmt.Println("  help       Show this message")
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "Unknown command: %s\nTry: menace unleash\n", os.Args[1])
			os.Exit(1)
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get working directory: %v\n", err)
		os.Exit(1)
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve executable path: %v\n", err)
		exe = cwd
	}
	menaceDir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(menaceDir, "prompts")); err != nil {
		menaceDir = cwd
	}

	mlog.Init(menaceDir)
	defer mlog.Close()

	s, err := store.Open(filepath.Join(menaceDir, "menace.db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = s.Close() }()

	if err := tui.Run(cwd, menaceDir, s); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
