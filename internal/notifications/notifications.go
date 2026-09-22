package notifications

import (
	"context"
	"github.com/godbus/dbus/v5"
	"strings"
	"time"
)

func Show(ctx context.Context, title, body string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	// Notification servers can interpret body markup. Escape it as plain text.
	body = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(body)
	return conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0, "TideSMS", uint32(0), "mail-message-new", title, body, []string{}, map[string]dbus.Variant{}, int32(5000)).Err
}
