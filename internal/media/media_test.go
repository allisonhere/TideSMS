package media

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pngFile(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetect(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	if got := Detect(env(map[string]string{"KITTY_WINDOW_ID": "1"})); got != Kitty {
		t.Fatalf("kitty = %v", got)
	}
	if got := Detect(env(map[string]string{"TERM_PROGRAM": "iTerm.app"})); got != ITerm {
		t.Fatalf("iterm = %v", got)
	}
	// Ghostty and WezTerm advertise Kitty graphics.
	if got := Detect(env(map[string]string{"TERM": "xterm-ghostty", "GHOSTTY_RESOURCES_DIR": "/usr/share/ghostty"})); got != Kitty {
		t.Fatalf("ghostty = %v", got)
	}
	if got := Detect(env(map[string]string{"TERM_PROGRAM": "WezTerm"})); got != Kitty {
		t.Fatalf("wezterm = %v", got)
	}
	if got := Detect(env(map[string]string{"TERM": "xterm-256color"})); got != None {
		t.Fatalf("plain = %v", got)
	}
	if Detect(nil) != None {
		t.Fatal("nil env should be none")
	}
}

func TestDescribeAndSize(t *testing.T) {
	cases := []struct {
		mime, file  string
		size        int64
		kind, label string
	}{
		{"image/jpeg", "dinner.jpg", 1_800_000, "Image", "1.8 MB"},
		{"application/pdf", "report.pdf", 2_400_000, "PDF document", "2.4 MB"},
		{"", "notes.txt", 12, "TXT file", "12 B"},
	}
	for _, c := range cases {
		kind, size := Describe(c.mime, c.file, c.size)
		if kind != c.kind || size != c.label {
			t.Errorf("Describe(%q,%q,%d) = %q,%q", c.mime, c.file, c.size, kind, size)
		}
	}
	if Dimensions(1920, 1080) != "1920×1080" || Dimensions(0, 5) != "" {
		t.Fatal("dimensions")
	}
}

func TestRenderOnlyForSupportedProtocols(t *testing.T) {
	path := pngFile(t)
	if w, h, ok := ImageSize(path); !ok || w != 4 || h != 2 {
		t.Fatalf("size = %d×%d ok=%v", w, h, ok)
	}
	kitty, ok := Render(Kitty, path, 10, 5)
	if !ok || !strings.Contains(kitty, "\x1b_G") || !strings.Contains(kitty, "a=T") {
		t.Fatalf("kitty = %q ok=%v", kitty, ok)
	}
	iterm, ok := Render(ITerm, path, 10, 5)
	if !ok || !strings.Contains(iterm, "1337;File") {
		t.Fatalf("iterm = %q ok=%v", iterm, ok)
	}
	if _, ok := Render(None, path, 10, 5); ok {
		t.Fatal("none should not render")
	}
	if _, ok := Render(Sixel, path, 10, 5); ok {
		t.Fatal("sixel is detected but not rendered here")
	}
	if _, ok := Render(Kitty, filepath.Join(t.TempDir(), "missing.png"), 10, 5); ok {
		t.Fatal("missing file should not render")
	}
}

func TestTextImageRendersHalfBlocks(t *testing.T) {
	path := pngFile(t)
	lines, ok := TextImage(path, 20, 5)
	if !ok || len(lines) == 0 {
		t.Fatalf("no art: ok=%v lines=%d", ok, len(lines))
	}
	if !strings.Contains(lines[0], "\u2580") {
		t.Fatalf("no block rune: %q", lines[0])
	}
	if _, ok := TextImage(filepath.Join(t.TempDir(), "missing.png"), 20, 5); ok {
		t.Fatal("missing file should not render")
	}
}

func TestBrailleImageRendersDots(t *testing.T) {
	path := pngFile(t)
	lines, ok := BrailleImage(path, 20, 5)
	if !ok || len(lines) == 0 {
		t.Fatalf("no braille: ok=%v lines=%d", ok, len(lines))
	}
	if !strings.ContainsFunc(lines[0], func(r rune) bool { return r >= 0x2800 && r <= 0x28FF }) {
		t.Fatalf("no braille rune: %q", lines[0])
	}
	if _, ok := BrailleImage(filepath.Join(t.TempDir(), "missing.png"), 20, 5); ok {
		t.Fatal("missing file should not render")
	}
}
