// Package media describes attachments and, where the terminal can, renders
// them. It performs no network I/O and never opens a file on its own.
package media

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	// Ghostty and WezTerm both implement the Kitty graphics protocol.
	if strings.Contains(program, "ghostty") || strings.Contains(program, "wezterm") ||
		strings.Contains(strings.ToLower(getenv("TERM")), "ghostty") ||
		getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return Kitty
	}
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

// textCache remembers rendered half-block art, keyed by file identity and size,
// so a conversation does not decode the same image on every frame.
var textCache sync.Map

// TextImage renders an image as half-block ANSI art sized to at most maxCols by
// maxRows cells, preserving aspect. Each cell carries two vertical pixels with
// U+2580. The result is ordinary text: it scrolls, frames and truncates like any
// other line, and needs no terminal graphics support.
func TextImage(path string, maxCols, maxRows int) ([]string, bool) {
	if path == "" || maxCols < 1 || maxRows < 1 {
		return nil, false
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return nil, false
	}
	key := path + "|" + strconv.FormatInt(fi.ModTime().UnixNano(), 10) + "|" + strconv.Itoa(maxCols) + "x" + strconv.Itoa(maxRows)
	if cached, ok := textCache.Load(key); ok {
		return cached.([]string), true
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, false
	}
	lines := halfBlocks(img, maxCols, maxRows)
	if len(lines) == 0 {
		return nil, false
	}
	textCache.Store(key, lines)
	return lines, true
}

func halfBlocks(img image.Image, maxCols, maxRows int) []string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return nil
	}
	// Fill the requested width even when the source is small: a thumbnail is
	// better shown large and soft than tiny and sharp.
	cols := max(1, maxCols)
	// A cell is roughly twice as tall as it is wide, so a cell pair of pixels
	// keeps the aspect ratio.
	rows := cols * h / (2 * w)
	if rows < 1 {
		rows = 1
	}
	if rows > maxRows {
		rows = maxRows
		if c := rows * 2 * w / h; c < cols {
			cols = max(1, c)
		}
	}
	// Box-average the source pixels that fall in each half-cell, so downscaled
	// detail stays readable instead of aliasing to noise.
	average := func(x0, x1, y0, y1 int) [3]uint8 {
		x0 = max(x0, b.Min.X)
		y0 = max(y0, b.Min.Y)
		x1 = min(x1, b.Max.X)
		y1 = min(y1, b.Max.Y)
		if x1 <= x0 || y1 <= y0 {
			x0, x1 = clampRange(x0, b.Min.X, b.Max.X)
			y0, y1 = clampRange(y0, b.Min.Y, b.Max.Y)
		}
		var sr, sg, sb, n uint64
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				r, g, bl, _ := img.At(x, y).RGBA()
				sr += uint64(r >> 8)
				sg += uint64(g >> 8)
				sb += uint64(bl >> 8)
				n++
			}
		}
		if n == 0 {
			return [3]uint8{}
		}
		return [3]uint8{uint8(sr / n), uint8(sg / n), uint8(sb / n)}
	}
	lines := make([]string, 0, rows)
	for row := 0; row < rows; row++ {
		var sb strings.Builder
		var lastTop, lastBottom [3]uint8
		have := false
		y0 := b.Min.Y + row*2*h/(rows*2)
		y1 := b.Min.Y + (row*2+2)*h/(rows*2)
		ymid := b.Min.Y + (row*2+1)*h/(rows*2)
		for cx := 0; cx < cols; cx++ {
			x0 := b.Min.X + cx*w/cols
			x1 := b.Min.X + (cx+1)*w/cols
			top := average(x0, x1, y0, ymid)
			bottom := average(x0, x1, ymid, y1)
			if !have || top != lastTop || bottom != lastBottom {
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm", top[0], top[1], top[2], bottom[0], bottom[1], bottom[2])
				lastTop, lastBottom, have = top, bottom, true
			}
			sb.WriteRune('▀')
		}
		sb.WriteString("\x1b[0m")
		lines = append(lines, sb.String())
	}
	return lines
}

func clampRange(v, lo, hi int) (int, int) {
	if v < lo {
		return lo, min(lo+1, hi)
	}
	if v >= hi {
		return max(hi-1, lo), hi
	}
	return v, min(v+1, hi)
}

// BrailleImage renders an image with braille dots: each cell holds a 2x4 grid
// of sub-pixels, eight times the detail of half-blocks for the same footprint.
// It is monochrome per cell, tinted with the cell's average colour, which reads
// far less blocky than large half-block cells.
func BrailleImage(path string, maxCols, maxRows int) ([]string, bool) {
	if path == "" || maxCols < 1 || maxRows < 1 {
		return nil, false
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return nil, false
	}
	key := "b|" + path + "|" + strconv.FormatInt(fi.ModTime().UnixNano(), 10) + "|" + strconv.Itoa(maxCols) + "x" + strconv.Itoa(maxRows)
	if cached, ok := textCache.Load(key); ok {
		return cached.([]string), true
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, false
	}
	lines := brailleLines(img, maxCols, maxRows)
	if len(lines) == 0 {
		return nil, false
	}
	textCache.Store(key, lines)
	return lines, true
}

// brailleBits maps a dot at (dx in 0..1, dy in 0..3) to its braille bit.
var brailleBits = [2][4]rune{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

func brailleLines(img image.Image, maxCols, maxRows int) []string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return nil
	}
	cols := max(1, maxCols)
	rows := cols * h / (2 * w)
	if rows < 1 {
		rows = 1
	}
	if rows > maxRows {
		rows = maxRows
	}
	lines := make([]string, 0, rows)
	for cy := 0; cy < rows; cy++ {
		var sb strings.Builder
		lastColor := [3]uint8{}
		colored := false
		for cx := 0; cx < cols; cx++ {
			var lum [2][4]float64
			var sum [3]uint64
			total := 0.0
			for dx := 0; dx < 2; dx++ {
				for dy := 0; dy < 4; dy++ {
					r, g, bl := samplePixel(img, b, cx*2+dx, cy*4+dy, cols*2, rows*4)
					l := 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(bl)
					lum[dx][dy] = l
					total += l
					if l >= 128 {
						sum[0] += uint64(r)
						sum[1] += uint64(g)
						sum[2] += uint64(bl)
					}
				}
			}
			mean := total / 8
			bits := rune(0)
			n := 0
			for dx := 0; dx < 2; dx++ {
				for dy := 0; dy < 4; dy++ {
					if lum[dx][dy] >= mean && lum[dx][dy] >= 24 {
						bits |= brailleBits[dx][dy]
						n++
					}
				}
			}
			if bits == 0 {
				sb.WriteRune(' ')
				continue
			}
			color := lastColor
			if n > 0 {
				color = [3]uint8{uint8(sum[0] / uint64(n)), uint8(sum[1] / uint64(n)), uint8(sum[2] / uint64(n))}
			}
			if !colored || color != lastColor {
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm", color[0], color[1], color[2])
				lastColor, colored = color, true
			}
			sb.WriteRune(0x2800 + bits)
		}
		sb.WriteString("\x1b[0m")
		lines = append(lines, sb.String())
	}
	return lines
}

// samplePixel maps a target pixel in a cols x rows grid onto the source, using
// the source pixel nearest the mapped centre.
func samplePixel(img image.Image, b image.Rectangle, px, py, cols, rows int) (uint8, uint8, uint8) {
	x := b.Min.X + (px*b.Dx()+cols/2)/cols
	y := b.Min.Y + (py*b.Dy()+rows/2)/rows
	if x >= b.Max.X {
		x = b.Max.X - 1
	}
	if y >= b.Max.Y {
		y = b.Max.Y - 1
	}
	r, g, bl, _ := img.At(x, y).RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)
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
			// Default cursor movement: the caption and hint print below the
			// image rather than over it.
			b.WriteString("a=T,f=100,c=" + strconv.Itoa(cols) + ",r=" + strconv.Itoa(rows) + ",")
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
