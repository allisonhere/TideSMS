// Command tidesms-daemon drains the outgoing queue and releases scheduled
// messages without requiring the terminal interface to stay open. It shares
// internal/storage, internal/backend and internal/messaging with the TUI.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/allisonhere/tidesms/internal/backend/kdeconnect"
	"github.com/allisonhere/tidesms/internal/clock"
	"github.com/allisonhere/tidesms/internal/config"
	"github.com/allisonhere/tidesms/internal/logging"
	"github.com/allisonhere/tidesms/internal/messaging"
	"github.com/allisonhere/tidesms/internal/queue"
	"github.com/allisonhere/tidesms/internal/scheduler"
	"github.com/allisonhere/tidesms/internal/storage"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tidesms-daemon:", err)
		os.Exit(1)
	}
}

func run() error {
	paths := config.DefaultPaths()
	showVersion := flag.Bool("version", false, "print version")
	once := flag.Bool("once", false, "process the queue a single time and exit")
	interval := flag.Duration("interval", 20*time.Second, "how often to check the queue and schedule")
	flag.StringVar(&paths.Config, "config", paths.Config, "config file")
	flag.StringVar(&paths.Database, "database", paths.Database, "SQLite state file")
	flag.StringVar(&paths.Log, "log", paths.Log, "structured log file")
	flag.Parse()
	if *showVersion {
		fmt.Println("tidesms-daemon", version)
		return nil
	}

	cfg, cfgErr := config.Load(paths.Config)
	log, file, err := logging.Open(paths.Log)
	if err != nil {
		log = slog.New(slog.NewJSONHandler(io.Discard, nil))
		if cfgErr == nil {
			cfgErr = fmt.Errorf("cannot open log file; check the state directory")
		}
	} else {
		defer func() { _ = file.Close() }()
	}
	if cfgErr != nil {
		log.Warn("configuration", "error", cfgErr.Error())
	}
	// Background sending is opt-in: the service does nothing until the user
	// enables it in TideSMS settings (or the config file).
	if !cfg.Scheduler.Enabled {
		fmt.Println("tidesms-daemon: background sending is disabled; enable it in TideSMS settings")
		return nil
	}

	// Only one sender may run against a state database. A second daemon (or a
	// stray manual run) exits instead of competing for the same queue.
	lockFile, err := os.OpenFile(paths.Database+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("cannot open lock file: %w", err)
	}
	defer func() { _ = lockFile.Close() }()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Println("tidesms-daemon: another instance is already running")
		return nil
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()

	store, err := storage.Open(paths.Database)
	if err != nil {
		return fmt.Errorf("cannot open SQLite: %w", err)
	}
	defer func() { _ = store.Close() }()

	backend := kdeconnect.New(log, cfg.Logging.DebugContent)
	c := clock.System{}
	scheduledMirror := mirrorScheduled(store, c)
	statusMirror := messaging.MirrorStatus(store)
	processor := &queue.Processor{
		Store:       store,
		Sender:      messaging.New(backend),
		Clock:       c,
		MaxAttempts: cfg.Queue.MaxAttempts,
		After: func(it queue.Item) {
			scheduledMirror(it)
			statusMirror(it)
		},
	}
	releaser := &scheduler.Releaser{Store: store, Queue: store, Clock: c}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pass := func() {
		if _, err := releaser.Release(c.Now()); err != nil {
			log.Error("release scheduled", "error", err.Error())
		}
		if _, err := processor.ProcessOnce(ctx, 10); err != nil && ctx.Err() == nil {
			log.Error("process queue", "error", err.Error())
		}
	}
	pass()
	if *once {
		return nil
	}
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			pass()
		}
	}
}

// mirrorScheduled copies a queue item's outcome back onto the scheduled row it
// came from, since both share an id.
func mirrorScheduled(store *storage.Store, c clock.Clock) func(queue.Item) {
	return func(item queue.Item) {
		s, ok, err := store.ScheduledItem(item.ID)
		if err != nil || !ok || s.State != scheduler.Queued {
			return
		}
		switch item.State {
		case queue.Sent:
			s.State = scheduler.Sent
		case queue.Failed:
			s.State = scheduler.Failed
			s.LastError = item.LastError
		default:
			return
		}
		s.UpdatedAt = c.Now()
		if err := store.UpdateScheduled(s); err != nil {
			return
		}
	}
}
