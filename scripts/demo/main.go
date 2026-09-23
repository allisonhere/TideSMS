// Command demo runs TideSMS on invented people and messages, with no phone
// involved. It is the real interface over the in-memory backend the tests use,
// so it can be tried without KDE Connect, and screenshots taken from it show
// nobody's real conversations. Every number is in the 555-01xx range reserved
// for fiction. Sending works, and goes nowhere.
//
//	go run ./scripts/demo            # a fresh demo in a temporary directory
//	go run ./scripts/demo DIR        # keep the demo's database and config in DIR
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/allisonhere/tidesms/internal/app"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/backend/fake"
	"github.com/allisonhere/tidesms/internal/config"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
)

const device = "demo_phone"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := ""
	if len(os.Args) > 1 {
		dir = os.Args[1]
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	} else {
		tmp, err := os.MkdirTemp("", "tidesms-demo-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		dir = tmp
	}
	cfgPath := filepath.Join(dir, "config.toml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	if cfg.KDEConnect.PreferredDevice == "" {
		cfg.General.Theme = "tokyo-night"
		cfg.KDEConnect.PreferredDevice = device
		cfg.Contacts.SyncFromPhone = false
		cfg.Notifications.Enabled = false
		if err := config.Save(cfgPath, cfg); err != nil {
			return err
		}
	}
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		return err
	}
	defer store.Close()

	phone := fake.New()
	phone.DeviceList = []backend.Device{{ID: device, Name: "Pixel 8", Connected: true, SMSCapability: "available",
		Capabilities: backend.Capabilities{SendText: true, ReceiveText: true, ReceiveMedia: true}}}
	now := time.Now().Truncate(time.Minute)
	for i, t := range threads() {
		// Names come from the address book, as they would for a real phone.
		if err := store.SaveContact(t.contact); err != nil {
			return err
		}
		thread := load(phone, i, t, now)
		phone.ThreadList = append(phone.ThreadList, thread)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := app.New(ctx, store, phone, cfg, cfgPath, slog.New(slog.NewJSONHandler(io.Discard, nil)), nil)
	// Ask for modified Enter keys, as the real binary does.
	fmt.Print("\x1b[>1u\x1b[>4;2m")
	defer fmt.Print("\x1b[<u\x1b[>4;0m")
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		return err
	}
	return model.FlushDrafts()
}

// load puts one conversation on the fake phone and returns its summary.
func load(phone *fake.Backend, i int, t thread, now time.Time) domain.Thread {
	p := domain.ParticipantFor(t.contact.PhoneNumber)
	backendID := fmt.Sprint(100 + i)
	id := domain.ThreadID(device, backendID)
	var ms []domain.Message
	for j, l := range t.lines {
		m := domain.Message{
			DeviceID: device, ThreadID: id, BackendID: fmt.Sprintf("%d-%d", i, j),
			Sender: p.Number, Body: l.body, Timestamp: now.Add(-l.ago),
			Direction: domain.Incoming, Status: domain.Unknown,
			Participants: []domain.Participant{p},
			Unread:       j >= len(t.lines)-t.unread,
		}
		if l.out {
			m.Sender, m.Direction, m.Status = "You", domain.Outgoing, domain.Submitted
		}
		ms = append(ms, m)
	}
	phone.History[id] = ms
	last := ms[len(ms)-1]
	return domain.Thread{ID: id, DeviceID: device, BackendID: backendID, LastMessage: last.Preview(),
		LastTimestamp: last.Timestamp, LastBackendID: last.BackendID, Participants: last.Participants}
}

type line struct {
	out  bool
	ago  time.Duration
	body string
}

type thread struct {
	contact contacts.Contact
	unread  int
	lines   []line
}

func person(id, name, number, theme string) contacts.Contact {
	return contacts.Contact{ID: id, Name: name, PhoneNumber: number, Theme: theme}
}

func threads() []thread {
	m, h := time.Minute, time.Hour
	d := 24 * h
	return []thread{
		{contact: person("maya", "Maya Ortiz", "+18165550142", "rose-pine"), lines: []line{
			{false, 3 * h, "Did you ever finish that book I lent you?"},
			{true, 3*h - 5*m, "Two chapters left. No spoilers!"},
			{false, 55 * m, "Are we still on for the trail tomorrow?"},
			{true, 50 * m, "Yes! Meet at the north lot at 8?"},
			{false, 49 * m, "Perfect. I'll bring coffee"},
			{false, 48 * m, "and the good granola bars"},
			{true, 30 * m, "You're the best"},
			{false, 12 * m, "Forecast says clear skies all morning"},
			{true, 11 * m, "Then it's a plan. See you at 8"},
		}},
		{contact: person("sam", "Sam Whitaker", "+18165550117", "nord"), unread: 2, lines: []line{
			{true, 3 * h, "Did the package ever show up?"},
			{false, 40 * m, "It did, finally"},
			{false, 39 * m, "Box was soaked but the lamp is fine"},
		}},
		{contact: person("june", "June Park", "+18165550188", ""), lines: []line{
			{false, 5 * h, "Band practice moved to Thursday"},
			{true, 5*h - 4*m, "Works for me, same time?"},
			{false, 5*h - 6*m, "Same time, bring the new chart"},
		}},
		{contact: person("dad", "Dad", "+18165550103", "gruvbox-dark"), lines: []line{
			{false, d + 2*h, "Tomatoes are finally coming in"},
			{true, d + h, "Save me some!"},
		}},
		{contact: person("theo", "Theo Brandt", "+18165550164", ""), lines: []line{
			{false, 2*d + 3*h, "Thanks for dinner last night"},
		}},
		{contact: person("library", "Riverside Library", "+18165550129", ""), lines: []line{
			{false, 3 * d, "Your hold on \"The Overstory\" is ready for pickup."},
		}},
		{contact: person("priya", "Priya Nair", "+18165550151", "tokyo-night"), lines: []line{
			{true, 4 * d, "Happy birthday!!"},
			{false, 4*d - h, "Thank you!!"},
		}},
	}
}
