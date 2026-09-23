package conversation

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tideui"
)

func BenchmarkHistoryLayout(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			r := tideui.NewRenderer(themes.Resolve("tide", "", ""), tideui.StyleOptions{})
			msgs := make([]domain.Message, count)
			for i := range msgs {
				msgs[i] = message(domain.Incoming, fmt.Sprintf("Message %d: loading history should keep typing and navigation responsive.", i))
			}
			m := New()
			m.SetMessages(msgs)
			opts := Options{Dates: true, Bubbles: true, Fill: true}
			m.Layout(r, 100, 30, opts)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.Layout(r, 100, 30, opts)
			}
		})
	}
}

func TestCachedHistoryMatchesFreshLayout(t *testing.T) {
	r := tideui.NewRenderer(themes.Resolve("tide", "", ""), tideui.StyleOptions{})
	opts := Options{Dates: true, Bubbles: true, Fill: true, Names: map[string]string{"+15551234567": "Alice"}}
	msgs := []domain.Message{message(domain.Incoming, "first message"), message(domain.Outgoing, "second message")}
	m := New()
	m.SetMessages(msgs)
	m.Layout(r, 80, 20, opts)
	check := func() {
		t.Helper()
		m.Layout(r, m.Width, 20, opts)
		fresh := New()
		fresh.SetMessages(m.Messages)
		fresh.Layout(r, m.Width, 20, opts)
		if !reflect.DeepEqual(m.lines, fresh.lines) || !reflect.DeepEqual(m.starts, fresh.starts) || !reflect.DeepEqual(m.ends, fresh.ends) {
			t.Fatal("cached layout differs from fresh layout")
		}
	}
	// Loading an older page must preserve the selected message and all headers.
	m.Move(-1)
	selected := m.Current().ID
	older := message(domain.Incoming, "older message")
	older.Timestamp = older.Timestamp.AddDate(0, 0, -1)
	msgs = append([]domain.Message{older}, msgs...)
	m.SetMessages(msgs)
	check()
	if m.Current().ID != selected {
		t.Fatal("loading older messages moved selection")
	}
	msgs[1].Unread = true
	msgs[2].Status = domain.Failed
	check()
	msgs[1].Body = "edited body with the same ID"
	check()
	opts.Names["+15551234567"] = "Renamed contact"
	check()
	opts.Query = "edited"
	check()
	// Explicitly resize through Layout so the old width remains the cache key.
	m.Layout(r, 30, 20, opts)
	check()
	r = tideui.NewRenderer(themes.Resolve("nord", "", ""), tideui.StyleOptions{})
	check()
	m.SetMessages(msgs[:1])
	check()
	if len(m.cache) != 1 {
		t.Fatal("cache retained messages outside the loaded window")
	}
}

func TestCachedImagesOnlyTransmitVisibleMessages(t *testing.T) {
	r := tideui.NewRenderer(themes.Resolve("tide", "", ""), tideui.StyleOptions{})
	photo := message(domain.Incoming, "photo")
	path := convTempPNG(t)
	photo.Attachments = []domain.Attachment{{ID: "image", MessageID: photo.ID, MIMEType: "image/png", LocalPath: path, State: domain.AttachmentAvailable}}
	msgs := []domain.Message{photo}
	for i := 0; i < 20; i++ {
		msgs = append(msgs, message(domain.Incoming, fmt.Sprint("later message ", i)))
	}
	opts := Options{InlineMedia: true, Graphics: true}
	m := New()
	m.SetMessages(msgs)
	m.Layout(r, 80, 10, opts)
	if m.Transmissions() != "" {
		t.Fatal("off-screen image transmitted")
	}
	m.Oldest()
	original := m.Transmissions()
	if original == "" {
		t.Fatal("visible image not transmitted")
	}
	m.Layout(r, 80, 10, opts)
	if m.Transmissions() != original {
		t.Fatal("cached image lost its transmission")
	}
	// Files can disappear without a database update; the cached image must expire.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m.Layout(r, 80, 10, opts)
	if m.Transmissions() != "" {
		t.Fatal("removed image still transmitted")
	}
	if !strings.Contains(strings.Join(m.lines, "\n"), "image:") {
		t.Fatal("missing media placeholder after image disappeared")
	}
}
