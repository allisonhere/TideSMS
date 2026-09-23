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
	// Bus lists paired devices from the daemon over D-Bus. It is asked first;
	// when it is nil or fails, the device list comes from kdeconnect-cli.
	Bus func(context.Context) ([]backend.Device, error)
	// SendMedia sends a message with attachments; nil uses the daemon's D-Bus
	// interface. Tests replace it so nothing reaches a phone.
	SendMedia func(ctx context.Context, device, phone, thread, text string, files []string) error
}

func New(log *slog.Logger, debug bool) *Client {
	return &Client{Log: log, DebugContent: debug, Probe: probeSMS, Bus: busDevices, Run: func(ctx context.Context, args ...string) ([]byte, error) {
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
	return c.devices(ctx, "discover")
}

// devices lists paired phones, over D-Bus when the daemon answers there and
// through kdeconnect-cli otherwise. operation names the caller in the log, so
// a failed check before a send can be told apart from a routine refresh.
func (c *Client) devices(ctx context.Context, operation string) ([]backend.Device, error) {
	if c.Bus != nil {
		ds, err := c.Bus(ctx)
		if err == nil {
			return ds, nil
		}
		if c.Log != nil {
			c.Log.Warn("kdeconnect D-Bus device lookup failed; using kdeconnect-cli", "operation", operation, "reason", err.Error())
		}
	}
	out, err := c.run(ctx, operation, "--list-devices")
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
		devices[i].Capabilities = capabilitiesFor(devices[i].SMSCapability)
	}
	return devices, nil
}

// capabilitiesFor maps the SMS plugin's availability onto the feature set.
// KDE Connect's SMS plugin carries text, group threads and received
// attachments; it cannot send media, and it reports no delivery receipt, so
// those stay false rather than being promised by the UI.
func capabilitiesFor(sms string) backend.Capabilities {
	if sms != "available" {
		return backend.Capabilities{}
	}
	return backend.Capabilities{
		SendText:     true,
		ReceiveText:  true,
		Groups:       true,
		ReceiveMedia: true,
		// Pictures go through the daemon's D-Bus interface, since the CLI
		// accepts --attachment but discards it (upstream leaves it a TODO).
		SendMedia:   true,
		ContactSync: true,
	}
}
func (c *Client) Send(ctx context.Context, req backend.SendRequest) error {
	phone, err := contacts.Normalize(req.PhoneNumber)
	if err != nil {
		return err
	}
	// A picture may go on its own; only a message with neither is empty.
	if strings.TrimSpace(req.Message) == "" && len(req.Attachments) == 0 {
		return errors.New("write a message before sending")
	}
	devices, err := c.devices(ctx, "send check")
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
	if len(req.Attachments) > 0 {
		send := c.SendMedia
		if send == nil {
			send = sendMedia
		}
		err := send(ctx, req.DeviceID, phone, req.ThreadID, req.Message, req.Attachments)
		if err == nil && c.Log != nil {
			c.Log.Info("MMS accepted by KDE Connect", "device", req.DeviceID, "attachments", len(req.Attachments))
		} else if err != nil && c.Log != nil {
			c.Log.Warn("kdeconnect request failed", "operation", "send media", "reason", err.Error())
		}
		return err
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
