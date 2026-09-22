package kdeconnect

import (
	"context"
	"errors"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/godbus/dbus/v5"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const contactsInterface = "org.kde.kdeconnect.device.contacts"

// vcardDir is where the KDE Connect contacts plugin caches the phone's address
// book, one vCard per contact. Upstream builds it from the generic data
// location, so the XDG variable is honoured the same way.
func vcardDir(device string) string {
	root := os.Getenv("XDG_DATA_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		root = filepath.Join(home, ".local/share")
	}
	return filepath.Join(root, "kpeoplevcard", "kdeconnect-"+device)
}

// SyncContacts asks the phone to refresh the local vCard cache, waits briefly
// for it to report completion, and then reads whatever the cache holds. The
// cache is read even when the request or the wait fails, so a phone that is
// slow or offline still yields the names it sent last time.
func (c *Client) SyncContacts(ctx context.Context, device string) ([]contacts.Synced, error) {
	if err := checkDevice(device); err != nil {
		return nil, err
	}
	dir := vcardDir(device)
	if dir == "" {
		return nil, errors.New("cannot locate the KDE Connect contact cache")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := requestContacts(ctx, device); err != nil && len(cached(dir)) == 0 {
		return nil, err
	}
	found := cached(dir)
	if len(found) == 0 {
		return nil, errors.New("the phone sent no contacts; in the KDE Connect Android app open this device's plugin settings, tap Contacts, and accept the per-device transfer confirmation")
	}
	return found, nil
}

// requestContacts triggers a refresh and waits for localCacheSynchronized,
// which the plugin emits once the vCards have been written to disk.
func requestContacts(ctx context.Context, device string) error {
	conn, err := connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	path := dbus.ObjectPath(string(devicePath(device)) + "/contacts")
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	if err = conn.AddMatchSignal(dbus.WithMatchSender(service), dbus.WithMatchObjectPath(path),
		dbus.WithMatchInterface(contactsInterface)); err != nil {
		return err
	}
	if err = conn.Object(service, path).CallWithContext(ctx, contactsInterface+".synchronizeRemoteWithLocal", 0).Err; err != nil {
		return errors.New("contact synchronization unavailable; enable the KDE Connect Contacts plugin for this phone")
	}
	for {
		select {
		case <-ctx.Done():
			return errors.New("the phone did not answer the contacts request; besides the Android Contacts permission, the Contacts plugin needs the per-device transfer confirmation accepted in the KDE Connect Android app")
		case s, ok := <-signals:
			if !ok {
				return errors.New("D-Bus connection closed")
			}
			if s != nil && s.Name == contactsInterface+".localCacheSynchronized" {
				return nil
			}
		}
	}
}

// cached parses every vCard the plugin has written for this device. Unreadable
// or malformed files are skipped so one bad contact cannot block the rest.
func cached(dir string) []contacts.Synced {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []contacts.Synced
	for _, e := range entries {
		name := e.Name()
		ext := filepath.Ext(name)
		if e.IsDir() || (ext != ".vcf" && ext != ".vcard") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() > 1<<20 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		out = append(out, contacts.ParseVCards(name[:len(name)-len(ext)], data)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PhoneNumber < out[j].PhoneNumber })
	return out
}
