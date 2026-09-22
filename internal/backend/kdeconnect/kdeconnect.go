package kdeconnect

import (
	"context"
	"errors"
	"fmt"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/contacts"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type Runner func(context.Context, ...string) ([]byte, error)
type Client struct {
	Run          Runner
	Log          *slog.Logger
	DebugContent bool
	Probe        func(context.Context, string) string
}

func New(log *slog.Logger, debug bool) *Client {
	return &Client{Log: log, DebugContent: debug, Probe: probeSMS, Run: func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "kdeconnect-cli", args...)
		cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "QT_LOGGING_RULES=*.debug=false")
		return cmd.CombinedOutput()
	}}
}
func (c *Client) run(ctx context.Context, operation string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	out, err := c.Run(ctx, args...)
	if err != nil || strings.HasPrefix(strings.TrimSpace(string(out)), "error:") {
		reason := "KDE Connect could not complete the request; check the phone’s SMS permission and plugin"
		switch {
		case errors.Is(err, exec.ErrNotFound):
			reason = "kdeconnect-cli is missing; install KDE Connect"
		case ctx.Err() != nil:
			reason = "KDE Connect timed out; check the daemon and phone connection"
		case strings.Contains(strings.ToLower(string(out)), "d-bus") || strings.Contains(strings.ToLower(string(out)), "dbus"):
			reason = "KDE Connect daemon is unavailable; start it in your desktop session"
		}
		// Never log arguments or subprocess output: either may contain SMS content.
		if c.Log != nil {
			c.Log.Warn("kdeconnect request failed", "operation", operation, "reason", reason)
		}
		return nil, errors.New(reason)
	}
	return out, nil
}

var deviceLine = regexp.MustCompile(`^- (.*): ([^ ]+)(?: on .*?)? \(([^)]+)\)$`)

func ParseDevices(data string) ([]backend.Device, error) {
	var devices []backend.Device
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		m := deviceLine.FindStringSubmatch(line)
		if m == nil && !strings.Contains(line, "(") {
			continue
		}
		if m == nil {
			return nil, fmt.Errorf("unrecognized KDE Connect device listing; check the installed CLI version")
		}
		state := m[3]
		if state != "paired" && state != "paired and reachable" {
			continue
		}
		devices = append(devices, backend.Device{ID: m[2], Name: contacts.SafeLabel(m[1]), Connected: state == "paired and reachable", SMSCapability: "unknown · enable SMS on phone"})
	}
	return devices, nil
}
func (c *Client) Devices(ctx context.Context) ([]backend.Device, error) {
	out, err := c.run(ctx, "discover", "--list-devices")
	if err != nil {
		return nil, err
	}
	devices, err := ParseDevices(string(out))
	if err != nil {
		return nil, err
	}
	for i := range devices {
		if devices[i].Connected && c.Probe != nil {
			devices[i].SMSCapability = c.Probe(ctx, devices[i].ID)
		}
	}
	return devices, nil
}
func (c *Client) Send(ctx context.Context, req backend.SendRequest) error {
	phone, err := contacts.Normalize(req.PhoneNumber)
	if err != nil {
		return err
	}
	if strings.TrimSpace(req.Message) == "" {
		return errors.New("write a message before sending")
	}
	devices, err := c.Devices(ctx)
	if err != nil {
		return err
	}
	online := false
	for _, d := range devices {
		if d.ID == req.DeviceID && d.Connected {
			online = true
			if d.SMSCapability == "unavailable" {
				return errors.New("SMS plugin unavailable — enable SMS in KDE Connect on both devices")
			}
		}
	}
	if !online {
		return errors.New("could not send — phone disconnected; reconnect it and retry")
	}
	if c.DebugContent && c.Log != nil {
		c.Log.Debug("outgoing SMS", "message", req.Message)
	}
	_, err = c.run(ctx, "send", "--device", req.DeviceID, "--send-sms", req.Message, "--destination", phone)
	if err == nil && c.Log != nil {
		c.Log.Info("SMS accepted by KDE Connect", "device", req.DeviceID)
	}
	return err
}

// probeSMS is an optional, read-only capability check. Sending stays exclusively
// on kdeconnect-cli. Missing busctl or older interfaces leave capability unknown.
func probeSMS(ctx context.Context, id string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "busctl", "--user", "call", "org.kde.kdeconnect", "/modules/kdeconnect/devices/"+id, "org.kde.kdeconnect.device", "hasPlugin", "s", "kdeconnect_sms").Output()
	if err != nil {
		return "unknown"
	}
	switch strings.TrimSpace(string(out)) {
	case "b true":
		return "available"
	case "b false":
		return "unavailable"
	}
	return "unknown"
}
