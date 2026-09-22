// Package media describes attachments and, where the terminal can, renders
// them. It performs no network I/O and never opens a file on its own.
package media

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Protocol is the terminal graphics capability the environment advertises.
type Protocol int

const (
	None Protocol = iota
	Kitty
	ITerm
	Sixel
)

func (p Protocol) String() string {
	switch p {
	case Kitty:
		return "kitty"
	case ITerm:
		return "iterm"
	case Sixel:
		return "sixel"
	default:
		return "none"
	}
}

// Detect reports the best available graphics protocol. It only looks at the
// environment, so it is safe to call at any time.
func Detect(getenv func(string) string) Protocol {
	if getenv == nil {
		return None
	}
	if getenv("KITTY_WINDOW_ID") != "" || getenv("TERM") == "xterm-kitty" {
		return Kitty
	}
	program := strings.ToLower(getenv("TERM_PROGRAM"))
	if strings.Contains(program, "iterm") || getenv("LC_TERMINAL") == "iTerm2" {
		return ITerm
	}
	term := strings.ToLower(getenv("TERM"))
	if strings.Contains(term, "sixel") || strings.Contains(term, "mlterm") {
		return Sixel
	}
	return None
}

// Describe returns a human label for the file kind and a human size, so a
// viewer can show "PDF document · 2.4 MB" without opening anything.
func Describe(mime, filename string, size int64) (kind, sizeLabel string) {
	return kindOf(mime, filename), HumanSize(size)
}

func kindOf(mime, filename string) string {
	mime = strings.ToLower(mime)
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "Image"
	case mime == "application/pdf":
		return "PDF document"
	case strings.HasPrefix(mime, "video/"):
		return "Video"
	case strings.HasPrefix(mime, "audio/"):
		return "Audio"
	case strings.HasPrefix(mime, "text/"):
		return "Text"
	}
	if ext := strings.ToLower(filepath.Ext(filename)); ext != "" {
		return strings.ToUpper(strings.TrimPrefix(ext, ".")) + " file"
	}
	return "File"
}

// HumanSize formats bytes in decimal units, as file managers do: 1.8 MB means
// 1,800,000 bytes.
func HumanSize(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1000*1000:
		return trimZero(float64(n)/1000) + " KB"
	case n < 1000*1000*1000:
		return trimZero(float64(n)/(1000*1000)) + " MB"
	default:
		return trimZero(float64(n)/(1000*1000*1000)) + " GB"
	}
}

func trimZero(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}

// Dimensions returns a "1920×1080" label for positive sizes and "" otherwise.
func Dimensions(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	return strconv.Itoa(w) + "×" + strconv.Itoa(h)
}

// Render encodes the image at path for the given protocol, scaled to cols×rows
// cells where the protocol supports it. It reports false when the protocol is
// unsupported or the file cannot be read as an image, so the caller falls back
// to text.
func Render(p Protocol, path string, cols, rows int) (string, bool) {
	if p != Kitty && p != ITerm || path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	pngData, ok := toPNG(data)
	if !ok {
		return "", false
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if p == Kitty {
		return kittySequence(pngData, cols, rows), true
	}
	return itermSequence(pngData, filepath.Base(path)), true
}

// toPNG returns PNG bytes for PNG or JPEG input, so both protocols can carry
// it (Kitty's f=100 is PNG, and iTerm accepts PNG too).
func toPNG(data []byte) ([]byte, bool) {
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err == nil {
		return data, true
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// ImageSize decodes just the header for width and height.
func ImageSize(path string) (int, int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer func() { _ = f.Close() }()
	if cfg, err := png.DecodeConfig(f); err == nil {
		return cfg.Width, cfg.Height, true
	}
	if _, err := f.Seek(0, 0); err != nil {
		return 0, 0, false
	}
	if cfg, err := jpeg.DecodeConfig(f); err == nil {
		return cfg.Width, cfg.Height, true
	}
	return 0, 0, false
}

const kittyChunk = 4096

func kittySequence(pngData []byte, cols, rows int) string {
	payload := base64.StdEncoding.EncodeToString(pngData)
	var b strings.Builder
	first := true
	for len(payload) > 0 {
		n := kittyChunk
		if n > len(payload) {
			n = len(payload)
		}
		chunk := payload[:n]
		payload = payload[n:]
		more := 1
		if len(payload) == 0 {
			more = 0
		}
		b.WriteString("\x1b_G")
		if first {
			b.WriteString("a=T,f=100,C=1,c=" + strconv.Itoa(cols) + ",r=" + strconv.Itoa(rows) + ",")
			first = false
		}
		b.WriteString("m=" + strconv.Itoa(more) + ";" + chunk + "\x1b\\")
	}
	return b.String()
}

func itermSequence(pngData []byte, name string) string {
	return "\x1b]1337;File=name=" + base64.StdEncoding.EncodeToString([]byte(name)) +
		";size=" + strconv.Itoa(len(pngData)) + ";inline=1:" +
		base64.StdEncoding.EncodeToString(pngData) + "\x07"
}
