package kdeconnect

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/domain"
)

// A message with pictures goes to the daemon's D-Bus interface, never to
// kdeconnect-cli, which would drop the pictures and send the text alone.
func TestPicturesNeverGoThroughTheCLI(t *testing.T) {
	var cli [][]string
	var got []string
	c := &Client{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			cli = append(cli, args)
			return []byte("- Pixel: phone (paired and reachable)"), nil
		},
		SendMedia: func(_ context.Context, device, phone, thread, text string, files []string) error {
			got = append([]string{device, phone, thread, text}, files...)
			return nil
		},
	}
	req := backend.SendRequest{DeviceID: "phone", PhoneNumber: "+1 555 123 4567", ThreadID: domain.ThreadID("phone", "42"),
		Message: "look", Attachments: []string{"/tmp/a.jpg"}}
	if err := c.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	want := []string{"phone", "+15551234567", domain.ThreadID("phone", "42"), "look", "/tmp/a.jpg"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("media send %q, want %q", got, want)
	}
	for _, args := range cli {
		if len(args) > 0 && args[0] != "--list-devices" {
			t.Fatalf("kdeconnect-cli was asked to send: %q", args)
		}
	}
}

// A picture can go with no text at all. This was refused as an empty message
// before the pictures were looked at, which only a real send showed: the app's
// tests use the fake phone, which does not make that check.
func TestAPictureNeedsNoText(t *testing.T) {
	sent := false
	c := &Client{
		Run: func(context.Context, ...string) ([]byte, error) {
			return []byte("- Pixel: phone (paired and reachable)"), nil
		},
		SendMedia: func(context.Context, string, string, string, string, []string) error { sent = true; return nil },
	}
	req := backend.SendRequest{DeviceID: "phone", PhoneNumber: "+15551234567", Attachments: []string{"/tmp/a.jpg"}}
	if err := c.Send(context.Background(), req); err != nil || !sent {
		t.Fatalf("picture without text: err %v, sent %v", err, sent)
	}
	req.Attachments = nil
	if err := c.Send(context.Background(), req); err == nil {
		t.Fatal("a message with neither text nor pictures was sent")
	}
}

// The phone's own thread id is used when there is one; a thread started here
// has none.
func TestPhoneThread(t *testing.T) {
	if id, ok := phoneThread("phone", domain.ThreadID("phone", "42")); !ok || id != 42 {
		t.Errorf("phone thread = %d %v", id, ok)
	}
	for _, thread := range []string{domain.ThreadID("phone", "local-+15551234567"), domain.ThreadID("other", "42"), "", "42"} {
		if _, ok := phoneThread("phone", thread); ok {
			t.Errorf("%q taken as the phone's thread", thread)
		}
	}
}

// The daemon sends an empty part for a file it cannot open, without saying
// so, so every file is checked before it is handed over.
func TestAttachmentArgsCheckEveryFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.jpg")
	empty := filepath.Join(dir, "empty.jpg")
	_ = os.WriteFile(good, []byte("jpeg"), 0o600)
	_ = os.WriteFile(empty, nil, 0o600)
	args, err := attachmentArgs([]string{good})
	if err != nil || len(args) != 1 || args[0].Value() != good {
		t.Fatalf("args %v err %v", args, err)
	}
	for name, files := range map[string][]string{
		"relative": {"a.jpg"},
		"missing":  {filepath.Join(dir, "gone.jpg")},
		"empty":    {empty},
		"folder":   {dir},
		"one bad":  {good, filepath.Join(dir, "gone.jpg")},
	} {
		if _, err := attachmentArgs(files); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), dir) {
			t.Errorf("%s: error names the full path: %v", name, err)
		}
	}
}

// Sends one real picture message. It runs only when both a device and a
// destination are given, and is meant for a number of your own.
//
//	TIDESMS_TEST_DEVICE=ID TIDESMS_TEST_MMS_TO=NUMBER go test ./internal/backend/kdeconnect -run TestLiveSendPicture -v
func TestLiveSendPicture(t *testing.T) {
	device, to := os.Getenv("TIDESMS_TEST_DEVICE"), os.Getenv("TIDESMS_TEST_MMS_TO")
	if device == "" || to == "" {
		t.Skip("opt-in: sends a real MMS")
	}
	path := filepath.Join(t.TempDir(), "tidesms-test.png")
	img := image.NewRGBA(image.Rect(0, 0, 96, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 96; x++ {
			img.Set(x, y, color.RGBA{uint8(40 + x), uint8(90 + y), 200, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	c := New(nil, false)
	err = c.Send(context.Background(), backend.SendRequest{DeviceID: device, PhoneNumber: to,
		Message: "TideSMS picture test", Attachments: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	// The daemon reads the file after the call returns; give it time before
	// the temporary directory goes.
	time.Sleep(5 * time.Second)
	t.Log("submitted; check the phone")
}
