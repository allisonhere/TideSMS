package domain

import "testing"

// A message that is only media is previewed by what it carries.
func TestPreviewNamesMedia(t *testing.T) {
	for _, tc := range []struct {
		msg  Message
		want string
	}{
		{Message{Body: "hi"}, "hi"},
		{Message{Body: "look", Attachments: []Attachment{{MIMEType: "image/jpeg"}}}, "look"},
		{Message{Attachments: []Attachment{{MIMEType: "image/jpeg"}}}, "Photo"},
		{Message{Attachments: []Attachment{{MIMEType: "video/mp4"}}}, "Video"},
		{Message{Attachments: []Attachment{{MIMEType: "image/png"}, {MIMEType: "image/png"}}}, "2 attachments"},
		{Message{}, ""},
	} {
		if got := tc.msg.Preview(); got != tc.want {
			t.Errorf("Preview(%+v) = %q, want %q", tc.msg, got, tc.want)
		}
		if got := tc.msg.Thread().LastMessage; got != tc.want {
			t.Errorf("thread preview = %q, want %q", got, tc.want)
		}
	}
}
