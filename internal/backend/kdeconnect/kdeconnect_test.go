package kdeconnect

import (
	"bytes"
	"context"
	"errors"
	"github.com/allisonhere/tidesms/internal/backend"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseDevices(t *testing.T) {
	input := "- Office: Pixel: phone1 on 192.0.2.1 via LAN (paired and reachable)\n- Old phone: phone2 (paired)\n- Stranger: phone3 (reachable)\n- Gone: phone4 \n4 devices found\n"
	ds, err := ParseDevices(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[0].Name != "Office: Pixel" || !ds[0].Connected || ds[1].Connected {
		t.Fatalf("%+v", ds)
	}
	if _, err = ParseDevices("- broken (paired)"); err == nil {
		t.Fatal("malformed listing silently accepted")
	}
}
func TestSendUsesLiteralArgumentsAndDoesNotLogContent(t *testing.T) {
	message := "hello \"friend\"; $(touch /tmp/NEVER)\nSecond line 💜"
	var log bytes.Buffer
	var calls [][]string
	c := &Client{Log: slog.New(slog.NewJSONHandler(&log, nil)), Run: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{}, args...))
		if args[0] == "--list-devices" {
			return []byte("- Pixel: phone (paired and reachable)"), nil
		}
		return nil, nil
	}}
	err := c.Send(context.Background(), backend.SendRequest{DeviceID: "phone", PhoneNumber: "+1 (555) 123-4567", Message: message})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--device", "phone", "--send-sms", message, "--destination", "+15551234567"}
	if !reflect.DeepEqual(calls[1], want) {
		t.Fatalf("arguments: %q", calls)
	}
	if strings.Contains(log.String(), message) || strings.Contains(log.String(), "Second line") {
		t.Fatal("message leaked into log")
	}
}
func TestOfflineAndMissingPluginNeverSend(t *testing.T) {
	for _, tc := range []struct{ state, cap string }{{"paired", "unknown"}, {"paired and reachable", "unavailable"}} {
		t.Run(tc.cap, func(t *testing.T) {
			calls := 0
			c := &Client{Probe: func(context.Context, string) string { return tc.cap }, Run: func(_ context.Context, args ...string) ([]byte, error) {
				calls++
				return []byte("- Phone: id (" + tc.state + ")"), nil
			}}
			err := c.Send(context.Background(), backend.SendRequest{DeviceID: "id", PhoneNumber: "12345", Message: "hello"})
			if err == nil || calls != 1 {
				t.Fatalf("err %v calls %d", err, calls)
			}
		})
	}
}
func TestFailureDoesNotEchoOutput(t *testing.T) {
	var log bytes.Buffer
	c := &Client{Log: slog.New(slog.NewJSONHandler(&log, nil)), Run: func(context.Context, ...string) ([]byte, error) {
		return []byte("private message content"), errors.New("exit 1")
	}}
	_, err := c.Devices(context.Background())
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(log.String(), "private") {
		t.Fatal("unsafe error handling")
	}
}
func TestLiveDiscovery(t *testing.T) {
	id := os.Getenv("TIDESMS_TEST_DEVICE")
	if id == "" {
		t.Skip("opt-in read-only desktop integration")
	}
	c := New(slog.Default(), false)
	ds, err := c.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == id {
			if !d.Connected || d.SMSCapability != "available" {
				t.Fatalf("not ready: %+v", d)
			}
			return
		}
	}
	t.Fatal("expected paired phone not found")
}
