package kdeconnect

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachedVCardsSkipUnusableFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("7.vcf", "BEGIN:VCARD\nFN:Amy\nTEL:+15551234567\nEND:VCARD\n")
	write("8.vcard", "BEGIN:VCARD\nFN:Chris\nTEL:+15557654321\nEND:VCARD\n")
	write("notes.txt", "BEGIN:VCARD\nFN:Ignored\nTEL:+15550000000\nEND:VCARD\n")
	write("9.vcf", "this is not a vcard")
	if err := os.Mkdir(filepath.Join(dir, "sub.vcf"), 0700); err != nil {
		t.Fatal(err)
	}
	got := cached(dir)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].PhoneNumber != "+15551234567" || got[0].UID != "7" {
		t.Fatalf("%+v", got[0])
	}
	if got[1].UID != "8" {
		t.Fatalf("uid from filename: %+v", got[1])
	}
	if cached(filepath.Join(dir, "missing")) != nil {
		t.Fatal("missing cache directory should read as empty")
	}
}

func TestVCardDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/data")
	if got := vcardDir("abc123"); got != "/tmp/data/kpeoplevcard/kdeconnect-abc123" {
		t.Fatal(got)
	}
}

func TestLiveContacts(t *testing.T) {
	id := os.Getenv("TIDESMS_TEST_DEVICE")
	if id == "" {
		t.Skip("opt-in read-only contact integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := New(nil, false).SyncContacts(ctx, id)
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Logf("imported %d numbers from the phone (names redacted)", len(got))
	if len(got) == 0 {
		t.Fatal("no contacts returned")
	}
}
