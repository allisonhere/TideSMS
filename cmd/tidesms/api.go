package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/allisonhere/tidesms/internal/api"
	"github.com/allisonhere/tidesms/internal/backend/kdeconnect"
	"github.com/allisonhere/tidesms/internal/config"
	"github.com/allisonhere/tidesms/internal/logging"
	"github.com/allisonhere/tidesms/internal/reply"
	"github.com/allisonhere/tidesms/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
)

const apiUsage = `Usage: tidesms api COMMAND [flags]

Commands:
  threads    list conversations, newest first
  messages   show a conversation's recent messages
  send       reply in a conversation
  read       mark a conversation read

Every command prints JSON and takes --config, --database and --log.
Run "tidesms api COMMAND -h" for a command's own flags.
`

// pathFlags adds the path overrides every subcommand shares.
func pathFlags(fs *flag.FlagSet) *config.Paths {
	paths := config.DefaultPaths()
	fs.StringVar(&paths.Config, "config", paths.Config, "config file")
	fs.StringVar(&paths.Database, "database", paths.Database, "SQLite state file")
	fs.StringVar(&paths.Log, "log", paths.Log, "structured log file")
	return &paths
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// runAPI is `tidesms api …`: TideSMS for other programs.
func runAPI(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		_, err := fmt.Fprint(stdout, apiUsage)
		return err
	}
	cmd, args := args[0], args[1:]
	fs := flag.NewFlagSet("tidesms api "+cmd, flag.ContinueOnError)
	paths := pathFlags(fs)
	switch cmd {
	case "threads":
		limit := fs.Int("limit", 0, "most conversations to list; 0 lists them all")
		unread := fs.Bool("unread", false, "only conversations with unread messages")
		format := fs.String("format", "json", `"json", or "tidedeck" for a TideDeck panel document`)
		if err := fs.Parse(args); err != nil {
			return err
		}
		s, err := storage.Open(paths.Database)
		if err != nil {
			// A panel must never go blank, so TideDeck gets a document saying
			// what is wrong rather than an error it would show as a failure.
			if *format == "tidedeck" {
				return printJSON(stdout, api.DeckError("can't read TideSMS", "Run tidesms once so it can create its database."))
			}
			return err
		}
		defer func() { _ = s.Close() }()
		ts, err := api.ListThreads(s, *limit, *unread)
		if *format == "tidedeck" {
			if err != nil {
				return printJSON(stdout, api.DeckError("can't read conversations", err.Error()))
			}
			return printJSON(stdout, api.Deck(ts, time.Now(), *unread))
		}
		if err != nil {
			return err
		}
		return printJSON(stdout, ts)
	case "messages":
		thread := fs.String("thread", "", "conversation id, as `api threads` reports it")
		limit := fs.Int("limit", 20, "most recent messages to show")
		if err := fs.Parse(args); err != nil {
			return err
		}
		s, err := storage.Open(paths.Database)
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		ms, err := api.ListMessages(s, *thread, *limit)
		if err != nil {
			return err
		}
		return printJSON(stdout, ms)
	case "read":
		thread := fs.String("thread", "", "conversation id")
		if err := fs.Parse(args); err != nil {
			return err
		}
		s, err := storage.Open(paths.Database)
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		if _, err := api.FindThread(s, *thread); err != nil {
			return err
		}
		if err := api.MarkRead(s, *thread); err != nil {
			return err
		}
		return printJSON(stdout, map[string]any{"schemaVersion": api.SchemaVersion, "thread": *thread, "read": true})
	case "send":
		thread := fs.String("thread", "", "conversation id")
		text := fs.String("text", "", "the message")
		var pictures multiFlag
		fs.Var(&pictures, "attach", "a picture to send; repeat for several")
		if err := fs.Parse(args); err != nil {
			return err
		}
		s, err := storage.Open(paths.Database)
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		cfg, _ := config.Load(paths.Config)
		log, closeLog := openLog(paths.Log)
		defer closeLog()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		sent, err := api.Send(ctx, s, kdeconnect.New(log, cfg.Logging.DebugContent), *thread, *text, pictures)
		if err != nil {
			return err
		}
		return printJSON(stdout, sent)
	}
	return fmt.Errorf("unknown api command %q; run tidesms api help", cmd)
}

// runReply is `tidesms reply ID`: a small window for answering one
// conversation, which returns the terminal when the reply is sent or left.
func runReply(args []string) error {
	fs := flag.NewFlagSet("tidesms reply", flag.ContinueOnError)
	paths := pathFlags(fs)
	ids, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(ids) != 1 {
		return errors.New("usage: tidesms reply CONVERSATION-ID (ids come from tidesms api threads)")
	}
	cfg, _ := config.Load(paths.Config)
	s, err := storage.Open(paths.Database)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	log, closeLog := openLog(paths.Log)
	defer closeLog()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, err := reply.New(ctx, s, kdeconnect.New(log, cfg.Logging.DebugContent), cfg, ids[0])
	if err != nil {
		return err
	}
	if _, err := fmt.Fprint(os.Stdout, "\x1b[>1u\x1b[>4;2m"); err != nil {
		return err
	}
	defer func() { _, _ = fmt.Fprint(os.Stdout, "\x1b[<u\x1b[>4;0m") }()
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// parseInterspersed parses flags wherever they appear among the arguments,
// which the flag package alone stops doing at the first plain one, and
// returns the plain ones.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var plain []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return plain, nil
		}
		plain = append(plain, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func openLog(path string) (*slog.Logger, func()) {
	log, f, err := logging.Open(path)
	if err != nil {
		return slog.New(slog.NewJSONHandler(io.Discard, nil)), func() {}
	}
	return log, func() { _ = f.Close() }
}

// multiFlag collects a flag given more than once.
type multiFlag []string

func (m *multiFlag) String() string     { return fmt.Sprint(*m) }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
