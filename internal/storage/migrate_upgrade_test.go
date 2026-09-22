package storage

import (
	"os"
	"testing"
)

// Upgrading a real database in place must add the new table without disturbing
// what is already cached. Set TIDESMS_TEST_DB to a copy of a live database.
func TestUpgradeExistingDatabase(t *testing.T) {
	path := os.Getenv("TIDESMS_TEST_DB")
	if path == "" {
		t.Skip("opt-in migration check against a copied database")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	cs, err := s.Contacts()
	if err != nil {
		t.Fatal(err)
	}
	ts, err := s.Threads("")
	if err != nil {
		t.Fatal(err)
	}
	sy, err := s.SyncedContacts("")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("upgraded: %d contacts, %d threads, %d synced (content redacted)", len(cs), len(ts), len(sy))
}
