package storage

import (
	"github.com/allisonhere/tidesms/internal/contacts"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in: imports the KDE Connect vCard cache into a COPY of a real database and
// reports how many cached threads gain a name. Contact content is never printed.
// Reading the cache directly keeps this check free of any backend dependency.
func TestLiveOverlayNamesThreads(t *testing.T) {
	path, device := os.Getenv("TIDESMS_TEST_DB"), os.Getenv("TIDESMS_TEST_DEVICE")
	if path == "" || device == "" {
		t.Skip("opt-in overlay check against a copied database")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".local/share/kpeoplevcard", "kdeconnect-"+device)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no vCard cache at %s", dir)
	}
	var cs []contacts.Synced
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".vcf" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		cs = append(cs, contacts.ParseVCards(e.Name(), data)...)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	before, err := s.Threads(device)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReplaceSyncedContacts(device, cs); err != nil {
		t.Fatal(err)
	}
	after, err := s.Threads(device)
	if err != nil {
		t.Fatal(err)
	}
	named := 0
	for i := range after {
		if after[i].DisplayName != before[i].DisplayName {
			named++
		}
	}
	t.Logf("parsed %d numbers; %d of %d threads now show a name instead of a number", len(cs), named, len(after))
}
