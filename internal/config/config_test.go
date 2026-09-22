package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestAIDefaultsAreLocalAndOff(t *testing.T) {
	c := Default()
	if c.AI.Enabled {
		t.Fatal("AI should be off by default")
	}
	if c.AI.Provider != "disabled" || c.AI.DefaultPolicy != "local" {
		t.Fatalf("ai defaults = %+v", c.AI)
	}
	if !c.AI.InlineMarks {
		t.Fatal("inline marks should default on")
	}
	if !c.Notifications.ShowSender || c.Notifications.Privacy {
		t.Fatalf("notification defaults = %+v", c.Notifications)
	}
	if c.Queue.MaxAttempts != 5 {
		t.Fatalf("max attempts = %d", c.Queue.MaxAttempts)
	}
}

func TestInvalidAISettingsRejected(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"provider":     func(c *Config) { c.AI.Provider = "gpt" },
		"policy":       func(c *Config) { c.AI.DefaultPolicy = "maybe" },
		"missingmodel": func(c *Config) { c.AI.Enabled = true; c.AI.Provider = "ollama"; c.AI.Model = "" },
		"attempts":     func(c *Config) { c.Queue.MaxAttempts = 0 },
	} {
		p := filepath.Join(t.TempDir(), "config.toml")
		c := Default()
		mutate(&c)
		if err := Save(p, c); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "ai") && !strings.Contains(err.Error(), "queue") {
			t.Fatalf("%s: expected a validation error, got %v", name, err)
		}
	}
}

func TestBubbleThemeNamesValidated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c := Default()
	c.Conversation.IncomingTheme = "dracula"
	c.Conversation.OutgoingTheme = "gruvbox-light"
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got.Conversation.IncomingTheme != "dracula" || got.Conversation.OutgoingTheme != "gruvbox-light" {
		t.Fatalf("round trip: %+v err=%v", got.Conversation, err)
	}

	bad := Default()
	bad.Conversation.IncomingTheme = "not-a-theme"
	if err := Save(p, bad); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("unknown bubble theme accepted")
	}
}

func TestLocalProviderWithModelRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c := Default()
	c.AI.Enabled = true
	c.AI.Provider = "lmstudio"
	c.AI.Endpoint = "http://127.0.0.1:1234/v1"
	c.AI.Model = "qwen3.5"
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got.AI != c.AI {
		t.Fatalf("ai = %+v err=%v", got.AI, err)
	}
}
