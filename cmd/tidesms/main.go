package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/allisonhere/tidesms/internal/app"
	"github.com/allisonhere/tidesms/internal/backend/kdeconnect"
	"github.com/allisonhere/tidesms/internal/config"
	"github.com/allisonhere/tidesms/internal/logging"
	"github.com/allisonhere/tidesms/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
	"io"
	"log/slog"
	"os"
)

var version = "0.1.0-dev"

func main() {
	// Hidden subcommand: draw one image on the terminal while the TUI is
	// suspended. The running binary invokes itself this way for a real image.
	if len(os.Args) > 2 && os.Args[1] == "image" {
		if err := runImageViewer(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "TideSMS:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "TideSMS:", err)
		os.Exit(1)
	}
}
func run() (runErr error) {
	paths := config.DefaultPaths()
	showVersion := flag.Bool("version", false, "print version")
	flag.StringVar(&paths.Config, "config", paths.Config, "config file")
	flag.StringVar(&paths.Database, "database", paths.Database, "SQLite state file")
	flag.StringVar(&paths.Log, "log", paths.Log, "structured log file")
	flag.Parse()
	if *showVersion {
		fmt.Println("TideSMS", version)
		return nil
	}
	cfg, startErr := config.Load(paths.Config)
	log, f, err := logging.Open(paths.Log)
	if err != nil {
		log = slog.New(slog.NewJSONHandler(io.Discard, nil))
		if startErr == nil {
			startErr = fmt.Errorf("cannot open log file; check the state directory")
		}
	} else {
		defer func() {
			if err := f.Close(); err != nil && runErr == nil {
				runErr = err
			}
		}()
	}
	store, err := storage.Open(paths.Database)
	if err != nil {
		log.Error("open SQLite", "error", err.Error())
		if startErr == nil {
			startErr = fmt.Errorf("cannot open SQLite; check your data directory")
		}
	}
	var repo app.Repository
	if store != nil {
		repo = store
		defer func() {
			if err := store.Close(); err != nil && runErr == nil {
				runErr = err
			}
		}()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := app.New(ctx, repo, kdeconnect.New(log, cfg.Logging.DebugContent), cfg, paths.Config, log, startErr)
	// Request disambiguation for Ctrl+Enter; restore terminal protocols on all returns.
	if _, err := fmt.Fprint(os.Stdout, "\x1b[>1u\x1b[>4;2m"); err != nil {
		return err
	}
	defer func() { _, _ = fmt.Fprint(os.Stdout, "\x1b[<u\x1b[>4;0m") }() // Best-effort terminal restoration, even when output has closed.
	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	if flushErr := model.FlushDrafts(); flushErr != nil && err == nil {
		return fmt.Errorf("could not save final drafts: %w", flushErr)
	}
	return err
}
