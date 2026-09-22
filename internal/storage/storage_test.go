package storage

import (
	"github.com/allisonhere/tidesms/internal/contacts"
	"path/filepath"
	"testing"
)

func TestRestartAndOutOfOrderDraftSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := contacts.Contact{ID: "amy", Name: "Amy", PhoneNumber: "+15551234567", Theme: "rose"}
	if err = s.SaveContact(c); err != nil {
		t.Fatal(err)
	}
	for _, d := range []Draft{{"new", 2}, {"stale", 1}, {"", 3}, {"new", 2}} {
		if err = s.SaveDraft(c.PhoneNumber, d); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	cs, err := s.Contacts()
	if err != nil || len(cs) != 1 || cs[0] != c {
		t.Fatalf("contacts %v %v", cs, err)
	}
	ds, err := s.Drafts()
	if err != nil || ds[c.PhoneNumber].Body != "" || ds[c.PhoneNumber].Revision != 3 {
		t.Fatalf("drafts %v %v", ds, err)
	}
	if err = s.DeleteContact(c.ID); err != nil {
		t.Fatal(err)
	}
	cs, err = s.Contacts()
	if err != nil || len(cs) != 0 {
		t.Fatalf("delete %v %v", cs, err)
	}
}
