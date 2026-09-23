package kdeconnect

import (
	"bytes"
	"context"
	"errors"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/domain"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestThumbnailFileMaterialisesBase64(t *testing.T) {
	// A 1x1 PNG, base64, as the plugin's thumbnail field carries it.
	const b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR4nGNgAAIAAAUAAen63NgAAAAASUVORK5CYII="
	path := thumbnailFile("msg:1", 7, b64)
	if path == "" {
		t.Fatal("thumbnail not materialised")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("thumbnail file missing: %v", err)
	}
	if got := thumbnailFile("m", 1, "not!!base64"); got != "" {
		t.Fatalf("garbage thumbnail should not become a path, got %q", got)
	}
	if got := thumbnailFile("m", 1, ""); got != "" {
		t.Fatalf("empty thumbnail should yield no path, got %q", got)
	}
}

// A thumbnail is a preview of the part, never the part. Recording it as the
// attachment's local file is what previously made the conversation draw a
// 100x100 image and made a download refuse as already done, putting the real
// image out of reach.
func TestThumbnailIsNotTreatedAsTheAttachment(t *testing.T) {
	const b64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR4nGNgAAIAAAUAAen63NgAAAAASUVORK5CYII="
	atts := attachmentsFor("msg:42", []wireAttachment{
		{PartID: 7, MIME: "image/png", Thumbnail: b64, Identifier: "uid-7"},
	})
	if len(atts) != 1 {
		t.Fatalf("got %d attachments, want 1", len(atts))
	}
	a := atts[0]
	if a.ThumbPath == "" {
		t.Error("the thumbnail was not kept as a preview")
	}
	if a.LocalPath != "" {
		t.Errorf("the thumbnail was recorded as the part itself: %q", a.LocalPath)
	}
	if a.State == domain.AttachmentAvailable {
		t.Error("a part with only a thumbnail was reported as available, which refuses the download")
	}
	// The thumbnail's own size says nothing about the image that was sent.
	if a.Width != 0 || a.Height != 0 {
		t.Errorf("dimensions %dx%d were taken from the thumbnail", a.Width, a.Height)
	}
	if path, full := a.Preview(); path != a.ThumbPath || full {
		t.Errorf("Preview() = %q full=%v, want the thumbnail reported as not full", path, full)
	}
}

func TestCapabilitiesFollowSMSAvailability(t *testing.T) {
	off := capabilitiesFor("unavailable")
	if off.SendText || off.ReceiveText || off.ReceiveMedia {
		t.Fatalf("unavailable plugin advertised features: %+v", off)
	}
	on := capabilitiesFor("available")
	if !on.SendText || !on.ReceiveText || !on.Groups || !on.ReceiveMedia || !on.ContactSync {
		t.Fatalf("available plugin missing features: %+v", on)
	}
	// Pictures are sent over D-Bus; there is still no delivery report.
	if !on.SendMedia || on.DeliveryStatus {
		t.Fatalf("KDE Connect sends media but cannot report delivery: %+v", on)
	}
	if off.SendMedia {
		t.Fatalf("unavailable plugin advertised media sending: %+v", off)
	}
}

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
