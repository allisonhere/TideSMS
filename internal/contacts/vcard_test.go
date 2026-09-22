package contacts

import (
	"reflect"
	"testing"
)

// Android's contacts provider emits vCard 2.1: CRLF, quoted-printable names
// folded with a trailing "=", and TEL parameters without TYPE=.
func TestParseAndroidVCard21(t *testing.T) {
	card := "BEGIN:VCARD\r\n" +
		"VERSION:2.1\r\n" +
		"N;CHARSET=UTF-8;ENCODING=QUOTED-PRINTABLE:B=C3=A4cker;Ann=\r\n=C3=A4;;;\r\n" +
		"FN;CHARSET=UTF-8;ENCODING=QUOTED-PRINTABLE:Ann=C3=A4 B=C3=A4cker\r\n" +
		"TEL;CELL:+1 (555) 123-4567\r\n" +
		"TEL;HOME;VOICE:555.987.6543\r\n" +
		"X-KDECONNECT-TIMESTAMP:1758500000\r\n" +
		"END:VCARD\r\n"
	got := ParseVCards("42", []byte(card))
	want := []Synced{
		{UID: "42", Name: "Annä Bäcker", PhoneNumber: "+15551234567", RawNumber: "+1 (555) 123-4567"},
		{UID: "42", Name: "Annä Bäcker", PhoneNumber: "5559876543", RawNumber: "555.987.6543"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// Desktop exporters emit 3.0/4.0: LF, indented folding, grouped properties.
func TestParseVCard30FoldedAndGrouped(t *testing.T) {
	card := "BEGIN:VCARD\nVERSION:3.0\n" +
		"item1.FN:A very long display name that the\n  exporter folded\n" +
		"item1.TEL;TYPE=CELL:+44 20 7946 0958\n" +
		"UID:urn:uuid:9\n" +
		"END:VCARD\n"
	got := ParseVCards("fallback", []byte(card))
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	if got[0].Name != "A very long display name that the exporter folded" {
		t.Fatalf("folding: %q", got[0].Name)
	}
	if got[0].PhoneNumber != "+442079460958" || got[0].UID != "urn:uuid:9" {
		t.Fatalf("%+v", got[0])
	}
}

// A missing FN falls back to the structured name, then to the organisation.
func TestParseVCardNameFallbacks(t *testing.T) {
	for _, tc := range []struct{ card, want string }{
		{"BEGIN:VCARD\nN:Patel;Rina;Q;Dr;PhD\nTEL:5551112222\nEND:VCARD", "Dr Rina Q Patel PhD"},
		{"BEGIN:VCARD\nORG:Acme Dental;Reception\nTEL:5551112222\nEND:VCARD", "Acme Dental Reception"},
		{"BEGIN:VCARD\nTEL:5551112222\nEND:VCARD", ""},
	} {
		got := ParseVCards("1", []byte(tc.card))
		if len(got) != 1 || got[0].Name != tc.want {
			t.Fatalf("%+v want %q", got, tc.want)
		}
	}
}

// Several cards can share a file, duplicate numbers within a card collapse, and
// unusable entries are dropped rather than guessed at.
func TestParseVCardsMultipleAndUnusable(t *testing.T) {
	file := "BEGIN:VCARD\nFN:Amy\nTEL:+15551234567\nTEL:+1 555 123 4567\nEND:VCARD\n" +
		"BEGIN:VCARD\nFN:Chris\nTEL:+15557654321\nEND:VCARD\n" +
		"BEGIN:VCARD\nFN:No Number\nEMAIL:nobody@example.com\nEND:VCARD\n" +
		"BEGIN:VCARD\nFN:Bad Number\nTEL:not a number\nEND:VCARD\n" +
		"BEGIN:VCARD\nFN:Too Short\nTEL:12\nEND:VCARD\n"
	got := ParseVCards("x", []byte(file))
	if len(got) != 2 || got[0].Name != "Amy" || got[1].Name != "Chris" {
		t.Fatalf("%+v", got)
	}
}

// Nothing from a phone may carry terminal control sequences into the interface.
func TestParseVCardStripsControlCharacters(t *testing.T) {
	got := ParseVCards("1", []byte("BEGIN:VCARD\nFN:Ev\x1b[31mil\nTEL:5551112222\nEND:VCARD"))
	if len(got) != 1 || got[0].Name != "Ev[31mil" {
		t.Fatalf("%+v", got)
	}
}

func TestParseVCardEmptyInput(t *testing.T) {
	for _, in := range []string{"", "not a vcard", "BEGIN:VCARD\nEND:VCARD"} {
		if got := ParseVCards("1", []byte(in)); len(got) != 0 {
			t.Fatalf("%q produced %+v", in, got)
		}
	}
}

// Numbers written in different formats compare by their national portion, but
// only enough of it that unrelated numbers stay distinct.
func TestMatchKey(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"+18165550182", "8165550182"},
		{"8165550182", "8165550182"},
		{"(816) 555-0182", "8165550182"},
		{"1-816-555-0182", "8165550182"},
		{"+442079460958", "2079460958"},
		{"5550182", ""},
		{"911", ""},
		{"", ""},
	} {
		if got := MatchKey(tc.in); got != tc.want {
			t.Errorf("MatchKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if MatchKey("+18165550182") == MatchKey("+18165550183") {
		t.Fatal("distinct numbers collided")
	}
}
