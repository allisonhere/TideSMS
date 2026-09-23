package kdeconnect

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidesms/internal/backend"
)

// The daemon's own answer is used when it gives one, and kdeconnect-cli, which
// spends seconds on network discovery, is not run at all.
func TestDevicesPreferTheDaemon(t *testing.T) {
	cli := 0
	c := &Client{
		Bus: func(context.Context) ([]backend.Device, error) {
			return []backend.Device{{ID: "phone", Name: "Pixel", Connected: true, SMSCapability: "available"}}, nil
		},
		Run: func(context.Context, ...string) ([]byte, error) { cli++; return nil, nil },
	}
	ds, err := c.Devices(context.Background())
	if err != nil || len(ds) != 1 || ds[0].ID != "phone" {
		t.Fatalf("devices %+v, err %v", ds, err)
	}
	if cli != 0 {
		t.Errorf("kdeconnect-cli ran %d times although the daemon answered", cli)
	}
}

// When the daemon cannot be asked, the command line still works, and the log
// says which caller fell back.
func TestDevicesFallBackToTheCLI(t *testing.T) {
	var log bytes.Buffer
	c := &Client{
		Log: slog.New(slog.NewJSONHandler(&log, nil)),
		Bus: func(context.Context) ([]backend.Device, error) { return nil, errors.New("no bus") },
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			if args[0] == "--list-devices" {
				return []byte("- Pixel: phone (paired and reachable)"), nil
			}
			return nil, nil
		},
	}
	if err := c.Send(context.Background(), backend.SendRequest{DeviceID: "phone", PhoneNumber: "+15551234567", Message: "hi"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), `"operation":"send check"`) {
		t.Errorf("the fallback before a send is not labelled as one: %s", log.String())
	}
}

// A failed check before a send is logged as such, not as a routine refresh.
func TestSendCheckIsLabelled(t *testing.T) {
	var log bytes.Buffer
	c := &Client{Log: slog.New(slog.NewJSONHandler(&log, nil)), Run: func(context.Context, ...string) ([]byte, error) {
		return nil, errors.New("exit 1")
	}}
	if err := c.Send(context.Background(), backend.SendRequest{DeviceID: "phone", PhoneNumber: "+15551234567", Message: "hi"}); err == nil {
		t.Fatal("a failed device check still sent")
	}
	if !strings.Contains(log.String(), `"operation":"send check"`) || strings.Contains(log.String(), `"operation":"discover"`) {
		t.Errorf("log = %s", log.String())
	}
}

// Against the real daemon: the D-Bus answer matches what kdeconnect-cli
// reports, and arrives far sooner.
func TestLiveBusDevicesMatchTheCLI(t *testing.T) {
	id := os.Getenv("TIDESMS_TEST_DEVICE")
	if id == "" {
		t.Skip("opt-in read-only desktop integration")
	}
	start := time.Now()
	bus, err := busDevices(context.Background())
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := (&Client{Run: New(nil, false).Run, Probe: probeSMS}).Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	find := func(ds []backend.Device) *backend.Device {
		for i := range ds {
			if ds[i].ID == id {
				return &ds[i]
			}
		}
		return nil
	}
	b, c := find(bus), find(cli)
	if b == nil || c == nil {
		t.Fatalf("device missing: bus %+v, cli %+v", bus, cli)
	}
	if b.Name != c.Name || b.Connected != c.Connected || b.SMSCapability != c.SMSCapability || b.Capabilities != c.Capabilities {
		t.Errorf("bus %+v, cli %+v", *b, *c)
	}
	t.Logf("D-Bus lookup took %v", took)
}
