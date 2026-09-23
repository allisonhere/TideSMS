package kdeconnect

import (
	"context"
	"errors"
	"time"

	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/godbus/dbus/v5"
)

const (
	daemonPath   = dbus.ObjectPath("/modules/kdeconnect")
	daemonIface  = "org.kde.kdeconnect.daemon"
	deviceIface  = "org.kde.kdeconnect.device"
	propertyGet  = "org.freedesktop.DBus.Properties.Get"
	smsPlugin    = "kdeconnect_sms"
	unknownSMS   = "unknown · enable SMS on phone"
	busDeviceMax = 3 * time.Second
)

// busDevices asks the daemon which phones are paired, whether each is
// reachable, and whether its SMS plugin is loaded. It answers in milliseconds;
// `kdeconnect-cli --list-devices` spends two seconds on network discovery
// every time, and sometimes far longer, which every send used to wait on.
func busDevices(ctx context.Context) ([]backend.Device, error) {
	ctx, cancel := context.WithTimeout(ctx, busDeviceMax)
	defer cancel()
	conn, err := connect(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	var ids []string
	// devices(onlyReachable, onlyPaired): every paired phone, reachable or not.
	if err := conn.Object(service, daemonPath).CallWithContext(ctx, daemonIface+".devices", 0, false, true).Store(&ids); err != nil {
		return nil, errors.New("KDE Connect daemon is unavailable; start it in your desktop session")
	}
	out := make([]backend.Device, 0, len(ids))
	for _, id := range ids {
		// An identifier that cannot name a device path is skipped, as it
		// would be refused everywhere else.
		if checkDevice(id) != nil {
			continue
		}
		obj := conn.Object(service, devicePath(id))
		var reachable bool
		if err := property(ctx, obj, "isReachable", &reachable); err != nil {
			return nil, err
		}
		var name string
		if property(ctx, obj, "name", &name) != nil || name == "" {
			name = id
		}
		d := backend.Device{ID: id, Name: contacts.SafeLabel(name), Connected: reachable, SMSCapability: unknownSMS}
		if reachable {
			var has bool
			if err := obj.CallWithContext(ctx, deviceIface+".hasPlugin", 0, smsPlugin).Store(&has); err == nil {
				d.SMSCapability = "unavailable"
				if has {
					d.SMSCapability = "available"
				}
			} else {
				d.SMSCapability = "unknown"
			}
		}
		d.Capabilities = capabilitiesFor(d.SMSCapability)
		out = append(out, d)
	}
	return out, nil
}

// property reads one of a device's properties within ctx's deadline, which
// godbus's own GetProperty does not honour.
func property(ctx context.Context, obj dbus.BusObject, name string, into any) error {
	var v dbus.Variant
	if err := obj.CallWithContext(ctx, propertyGet, 0, deviceIface, name).Store(&v); err != nil {
		return err
	}
	return v.Store(into)
}
