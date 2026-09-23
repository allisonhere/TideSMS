package kdeconnect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/godbus/dbus/v5"
)

// conversationAddress is KDE Connect's ConversationAddress as it crosses
// D-Bus: a structure holding one string, signature (s).
type conversationAddress struct{ Address string }

// sendMedia sends a message with attachments through the daemon's
// conversations interface. kdeconnect-cli cannot: it accepts --attachment and
// then drops it. The daemon reads each attachment itself, from a plain path
// (it opens the string with QFile, so a file:// URL would not be found), and
// sends an empty part without complaint when it cannot, which is why every file
// is checked here first.
//
// A thread the phone already knows is answered with replyToConversation, which
// sends to its participants; a new one goes to the number with
// sendWithoutConversation. Both return as soon as the daemon has the request:
// like the CLI, success means submitted, not sent.
func sendMedia(ctx context.Context, device, phone, thread, text string, files []string) error {
	if err := checkDevice(device); err != nil {
		return err
	}
	attachments, err := attachmentArgs(files)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, err := connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if id, ok := phoneThread(device, thread); ok {
		err = call(ctx, conn, device, "replyToConversation", id, text, attachments).Err
	} else {
		if phone == "" {
			return errors.New("no number to send the picture to")
		}
		addresses := []dbus.Variant{dbus.MakeVariant(conversationAddress{phone})}
		err = call(ctx, conn, device, "sendWithoutConversation", addresses, text, attachments).Err
	}
	if err != nil {
		return errors.New("KDE Connect could not send the picture; check that the SMS plugin is enabled")
	}
	return nil
}

// phoneThread is the phone's own id for a thread, when it has one. A thread
// started here is "local-" until the phone reports it.
func phoneThread(device, thread string) (int64, bool) {
	id := strings.TrimPrefix(thread, domain.ThreadID(device, ""))
	if id == thread || id == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(id, 10, 64)
	return n, err == nil
}

// attachmentArgs checks that every file can be read and returns their
// absolute paths as the variants the daemon expects.
func attachmentArgs(files []string) ([]dbus.Variant, error) {
	out := make([]dbus.Variant, 0, len(files))
	for _, f := range files {
		if !filepath.IsAbs(f) {
			return nil, errors.New("attachment path must be absolute")
		}
		fh, err := os.Open(f)
		if err != nil {
			return nil, errors.New("could not read " + filepath.Base(f) + "; attach it again")
		}
		info, err := fh.Stat()
		_ = fh.Close()
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return nil, errors.New(filepath.Base(f) + " is not a file that can be sent")
		}
		out = append(out, dbus.MakeVariant(f))
	}
	return out, nil
}
