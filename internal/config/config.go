package config

import (
	"bytes"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/allisonhere/tidesms/internal/ai"
	"github.com/allisonhere/tidesms/internal/themes"
	"os"
	"path/filepath"
)

type Config struct {
	Sync struct {
		InitialMessages int `toml:"initial_messages"`
		PageSize        int `toml:"page_size"`
	} `toml:"sync"`
	Notifications struct {
		Enabled    bool `toml:"enabled"`
		ShowSender bool `toml:"show_sender"`
		ShowBody   bool `toml:"show_body"`
		Sound      bool `toml:"sound"`
		// Privacy shows the sender only, never the message contents.
		Privacy bool `toml:"privacy"`
	} `toml:"notifications"`
	Contacts struct {
		SyncFromPhone bool `toml:"sync_from_phone"`
	} `toml:"contacts"`
	Conversation struct {
		Timestamps         string `toml:"timestamps"`
		ShowDateSeparators bool   `toml:"show_date_separators"`
		MaxWidth           int    `toml:"max_width"`
		Bubbles            bool   `toml:"bubbles"`
		Corners            string `toml:"corners"`
		FillBubbles        bool   `toml:"fill_bubbles"`
		// IncomingTheme and OutgoingTheme are the global bubble palettes for the
		// two directions. Empty keeps the surface derived from the pane theme.
		IncomingTheme string `toml:"incoming_theme"`
		OutgoingTheme string `toml:"outgoing_theme"`
	} `toml:"conversation"`

	General struct {
		Theme         string `toml:"theme"`
		CompactStatus bool   `toml:"compact_status"`
	} `toml:"general"`
	Composer struct {
		Mode string `toml:"mode"`
	} `toml:"composer"`
	KDEConnect struct {
		PreferredDevice string `toml:"preferred_device"`
	} `toml:"kdeconnect"`
	AI struct {
		Enabled bool `toml:"enabled"`
		// Provider is one of the values in ai.Providers (ollama, lmstudio, openai,
		// anthropic, deepseek, custom-openai-compatible). "disabled" keeps the
		// writer unavailable without discarding the rest of the settings.
		Provider string `toml:"provider"`
		Endpoint string `toml:"endpoint"`
		Model    string `toml:"model"`
		// APIKey is only needed by cloud providers. It is never logged.
		APIKey string `toml:"api_key"`
		// DefaultPolicy is the privacy default for threads without an override:
		// "local", "cloud" or "disabled".
		DefaultPolicy string `toml:"default_policy"`
		// InlineMarks draws subtle markers under text the assistant flagged.
		InlineMarks bool `toml:"inline_marks"`
	} `toml:"ai"`
	Queue struct {
		// MaxAttempts caps automatic retries before a message is left failed.
		MaxAttempts int `toml:"max_attempts"`
	} `toml:"queue"`
	Scheduler struct {
		// Enabled permits the optional background sender to run.
		Enabled bool `toml:"enabled"`
	} `toml:"scheduler"`
	Logging struct {
		DebugContent bool `toml:"debug_content"`
	} `toml:"logging"`
}

func Default() Config {
	var c Config
	c.General.Theme = "tide"
	c.Composer.Mode = "normal"
	c.Sync.InitialMessages = 100
	c.Sync.PageSize = 100
	c.Notifications.Enabled = true
	c.Notifications.ShowSender = true
	c.Notifications.ShowBody = true
	c.Contacts.SyncFromPhone = true
	c.AI.Provider = string(ai.ProviderDisabled)
	c.AI.DefaultPolicy = string(ai.PolicyLocal)
	c.AI.InlineMarks = true
	c.Queue.MaxAttempts = 5
	c.Conversation.Timestamps = "smart"
	c.Conversation.ShowDateSeparators = true
	c.Conversation.MaxWidth = 0 // Use the pane.
	c.Conversation.Bubbles = true
	c.Conversation.Corners = "round"
	c.Conversation.FillBubbles = true
	return c
}
func Load(path string) (Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, Save(path, c)
	}
	if err != nil {
		return c, err
	}
	if _, err = toml.Decode(string(b), &c); err != nil {
		return Default(), fmt.Errorf("malformed configuration; using defaults (file preserved)")
	}
	if !themes.Valid(c.General.Theme) || (c.Composer.Mode != "normal" && c.Composer.Mode != "vim") {
		return Default(), fmt.Errorf("invalid theme or composer mode in configuration; using defaults (file preserved)")
	}
	if c.Sync.InitialMessages < 1 || c.Sync.InitialMessages > 1000 || c.Sync.PageSize < 1 || c.Sync.PageSize > 1000 {
		return Default(), fmt.Errorf("sync batch sizes must be between 1 and 1000")
	}
	if c.Conversation.Timestamps != "smart" && c.Conversation.Timestamps != "full" {
		return Default(), fmt.Errorf("conversation timestamps must be smart or full")
	}
	if c.Conversation.MaxWidth < 0 || c.Conversation.MaxWidth > 500 {
		return Default(), fmt.Errorf("conversation max_width must be between 0 and 500")
	}
	if c.Conversation.Corners != "round" && c.Conversation.Corners != "square" {
		return Default(), fmt.Errorf("conversation corners must be round or square")
	}
	if !validThemeName(c.Conversation.IncomingTheme) || !validThemeName(c.Conversation.OutgoingTheme) {
		return Default(), fmt.Errorf("conversation bubble themes must be empty or a known theme name")
	}
	if !ai.ValidProvider(c.AI.Provider) {
		return Default(), fmt.Errorf("unknown ai provider %q", c.AI.Provider)
	}
	if !ai.ValidPolicy(c.AI.DefaultPolicy) {
		return Default(), fmt.Errorf("ai default_policy must be local, cloud or disabled")
	}
	if c.AI.Enabled && c.AI.Provider != string(ai.ProviderDisabled) && c.AI.Model == "" {
		return Default(), fmt.Errorf("ai model must be set when a provider is enabled")
	}
	if c.Queue.MaxAttempts < 1 || c.Queue.MaxAttempts > 50 {
		return Default(), fmt.Errorf("queue max_attempts must be between 1 and 50")
	}
	return c, nil
}
func validThemeName(name string) bool { return name == "" || themes.Valid(name) }

func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(c); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }() // Temporary file may have been renamed.
	if _, err = f.Write(b.Bytes()); err != nil {
		_ = f.Close() // Preserve the original write error.
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

type Paths struct{ Config, Database, Log string }

func DefaultPaths() Paths {
	home, _ := os.UserHomeDir()
	root := func(env, fallback string) string {
		if p := os.Getenv(env); p != "" {
			return p
		}
		return filepath.Join(home, fallback)
	}
	return Paths{filepath.Join(root("XDG_CONFIG_HOME", ".config"), "tidesms/config.toml"), filepath.Join(root("XDG_DATA_HOME", ".local/share"), "tidesms/state.db"), filepath.Join(root("XDG_STATE_HOME", ".local/state"), "tidesms/tidesms.log")}
}
