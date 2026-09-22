package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRoundTripAndMalformedPreservation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	c.Composer.Mode = "vim"
	c.General.Theme = "rose"
	c.KDEConnect.PreferredDevice = "phone"
	if err = Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got != c {
		t.Fatalf("%+v %v", got, err)
	}
	bad := []byte("[composer\nmode=oops")
	if err = os.WriteFile(p, bad, 0600); err != nil {
		t.Fatal(err)
	}
	got, err = Load(p)
	if err == nil || got != Default() {
		t.Fatal("expected safe defaults and error")
	}
	b, _ := os.ReadFile(p)
	if string(b) != string(bad) {
		t.Fatal("malformed file overwritten")
	}
}
