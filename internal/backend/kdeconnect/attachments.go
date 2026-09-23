package kdeconnect

import (
	"context"
	"errors"
	"time"
)

// FetchAttachment asks the phone for a message part's file and waits for the
// daemon's attachmentReceived signal. The returned path is the daemon's cached
// copy; callers should treat it as read-only.
func (c *Client) FetchAttachment(ctx context.Context, device string, partID int64, uniqueIdentifier string) (string, error) {
	if err := checkDevice(device); err != nil {
		return "", err
	}
	if uniqueIdentifier == "" {
		return "", errors.New("this attachment has no identifier to request")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	conn, err := connect(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	ch, err := signals(conn, device)
	if err != nil {
		return "", err
	}
	defer conn.RemoveSignal(ch)

	if err := call(ctx, conn, device, "requestAttachmentFile", partID, uniqueIdentifier).Err; err != nil {
		return "", errors.New("this phone could not fetch the attachment")
	}
	for {
		select {
		case <-ctx.Done():
			return "", errors.New("timed out waiting for the attachment")
		case sig, ok := <-ch:
			// The channel closes with the connection; a nil signal would
			// otherwise be read as one.
			if !ok || sig == nil {
				return "", errors.New("KDE Connect disconnected while fetching the attachment")
			}
			if sig.Name != conversations+".attachmentReceived" || len(sig.Body) < 1 {
				continue
			}
			path, _ := sig.Body[0].(string)
			if path == "" {
				continue
			}
			// The file name is the unique identifier; ignore an unrelated
			// attachment that happened to arrive first.
			if len(sig.Body) >= 2 {
				if name, _ := sig.Body[1].(string); name != "" && name != uniqueIdentifier {
					continue
				}
			}
			return path, nil
		}
	}
}
