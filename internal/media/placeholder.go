package media

import (
	"bytes"
	"encoding/base64"
	"hash/fnv"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The Kitty graphics protocol can place an image into the text grid rather than
// at an absolute pixel position: the image is transmitted with a virtual
// placement, and ordinary cells holding U+10EEEE then say which part of it to
// draw. Each such cell carries two combining marks giving its row and column
// within the image, and a foreground colour carrying the image's id.
//
// That indirection is what makes graphics usable inside a TUI pane. The cells
// are ordinary text one column wide, so the pane's padding, truncation, scroll
// and background all act on them exactly as they act on a letter; a direct
// placement, by contrast, is painted over the terminal and is cut apart by the
// first pane that pads a line.
const placeholder = rune(0x10EEEE)

// maxPlaceholderCells is the largest row or column a placeholder can name.
var maxPlaceholderCells = len(rowColumnDiacritics)

// Placeholders reports whether the protocol can draw an image inside a pane.
// Only Kitty defines Unicode placeholders; iTerm2's protocol paints over the
// terminal and cannot survive a pane, so it is excluded even though Render
// supports it for the full-screen viewer.
func Placeholders(p Protocol) bool { return p == Kitty }

// PlacementRows reports how many cell rows an image that is cols wide occupies
// at its own aspect ratio. cellW and cellH are the terminal's cell size in
// pixels; when they are unknown, a cell is assumed to be twice as tall as it is
// wide, which is the usual proportion and the one the text renderers assume.
func PlacementRows(imgW, imgH, cols, cellW, cellH int) int {
	if imgW < 1 || imgH < 1 || cols < 1 {
		return 0
	}
	if cellW < 1 || cellH < 1 {
		cellW, cellH = 1, 2
	}
	rows := (imgH*cols*cellW + imgW*cellH/2) / (imgW * cellH)
	if rows < 1 {
		rows = 1
	}
	return rows
}

// PlacementBox returns the largest cell box no bigger than maxCols by maxRows
// that carries the image at its own proportions. Sizing the box to the image
// rather than handing the terminal a fixed rectangle is what keeps a tall photo
// from leaving a band of dead cells beside it: the terminal fits the image
// inside whatever box it is given, and only a box of the right shape is filled.
func PlacementBox(imgW, imgH, maxCols, maxRows, cellW, cellH int) (cols, rows int) {
	if imgW < 1 || imgH < 1 || maxCols < 1 || maxRows < 1 {
		return 0, 0
	}
	cols = maxCols
	rows = PlacementRows(imgW, imgH, cols, cellW, cellH)
	if rows <= maxRows {
		return cols, rows
	}
	// Too tall for the space: keep the height and take back the width, rather
	// than cropping or squeezing the image.
	rows = maxRows
	if cellW < 1 || cellH < 1 {
		cellW, cellH = 1, 2
	}
	cols = (imgW*rows*cellH + imgH*cellW/2) / (imgH * cellW)
	if cols < 1 {
		cols = 1
	}
	if cols > maxCols {
		cols = maxCols
	}
	return cols, rows
}

// ViewerBox returns the cell box a full-screen view should ask for: the image
// at its own proportions, never larger than its own pixels and never larger
// than the space available.
//
// The no-upscaling rule is the point. Handing the terminal the whole screen
// makes it stretch whatever it is given to fill it, so a small attachment is
// blown up past its own resolution and shown soft — an enlarged thumbnail
// rather than the picture. Below its native size an image is scaled down, which
// is honest; above it there is no more detail to show.
//
// It falls back to the full space when the cell size is unknown, since without
// it there is no way to tell how many cells the image's pixels occupy.
func ViewerBox(imgW, imgH, maxCols, maxRows, cellW, cellH int) (cols, rows int) {
	if imgW < 1 || imgH < 1 || maxCols < 1 || maxRows < 1 {
		return 0, 0
	}
	if cellW < 1 || cellH < 1 {
		return maxCols, maxRows
	}
	// The cells the image covers at one image pixel per screen pixel, rounded
	// up so a partly filled cell is still available to it.
	nativeCols := (imgW + cellW - 1) / cellW
	nativeRows := (imgH + cellH - 1) / cellH
	return PlacementBox(imgW, imgH, min(maxCols, nativeCols), min(maxRows, nativeRows), cellW, cellH)
}

// InlineImage returns the escape that transmits the image at path and the cell
// lines that draw it, sized to cols by rows. The transmission is a zero-width
// APC sequence that must be written outside any padded pane content; the lines
// are ordinary text and belong wherever the image should appear.
//
// It reports false when the file cannot be read as an image, or when cols or
// rows exceed what a placeholder can address, so the caller falls back to text.
func InlineImage(path string, cols, rows int) (transmit string, lines []string, ok bool) {
	if path == "" || cols < 1 || rows < 1 {
		return "", nil, false
	}
	if cols > maxPlaceholderCells || rows > maxPlaceholderCells {
		return "", nil, false
	}
	id := imageID(path, cols, rows)
	transmit, ok = transmission(path, id, cols, rows)
	if !ok {
		return "", nil, false
	}
	return transmit, placeholderLines(id, cols, rows), true
}

// transmission encodes one image for a virtual placement. A PNG already on disk
// is sent by path, which keeps the escape around a hundred bytes however large
// the image is; anything else is converted and sent inline, since the terminal
// has no file it could read instead.
func transmission(path string, id uint32, cols, rows int) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	header := "a=T,U=1,i=" + strconv.FormatUint(uint64(id), 10) +
		",f=100,c=" + strconv.Itoa(cols) + ",r=" + strconv.Itoa(rows) + ",q=2"
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err == nil {
		// t=f names a file the terminal reads itself and leaves in place; only
		// t=t would invite it to delete the file, and these are cached
		// attachments the user still owns.
		abs := path
		if resolved, err := filepath.Abs(path); err == nil {
			abs = resolved
		}
		return "\x1b_G" + header + ",t=f;" + base64.StdEncoding.EncodeToString([]byte(abs)) + "\x1b\\", true
	}
	pngData, ok := toPNG(data)
	if !ok {
		return "", false
	}
	return chunked(header, base64.StdEncoding.EncodeToString(pngData)), true
}

// chunked splits a payload across escapes, as the protocol requires for
// anything over a few kilobytes. Only the first escape carries the key-value
// header; the rest carry m= alone.
func chunked(header, payload string) string {
	var b strings.Builder
	b.Grow(len(payload) + len(payload)/kittyChunk*16 + len(header) + 16)
	first := true
	for len(payload) > 0 {
		n := min(kittyChunk, len(payload))
		chunk := payload[:n]
		payload = payload[n:]
		more := 0
		if len(payload) > 0 {
			more = 1
		}
		b.WriteString("\x1b_G")
		if first {
			b.WriteString(header + ",")
			first = false
		}
		b.WriteString("m=" + strconv.Itoa(more) + ";" + chunk + "\x1b\\")
	}
	return b.String()
}

// placeholderLines draws the cells for one placement. The image id travels in
// the foreground colour, so a single colour is set for each row and every cell
// in it names only its own row and column.
func placeholderLines(id uint32, cols, rows int) []string {
	r, g, b := (id>>16)&0xFF, (id>>8)&0xFF, id&0xFF
	prefix := "\x1b[38;2;" + strconv.FormatUint(uint64(r), 10) + ";" +
		strconv.FormatUint(uint64(g), 10) + ";" + strconv.FormatUint(uint64(b), 10) + "m"
	lines := make([]string, 0, rows)
	for y := 0; y < rows; y++ {
		var sb strings.Builder
		sb.Grow(len(prefix) + cols*12 + 4)
		sb.WriteString(prefix)
		for x := 0; x < cols; x++ {
			sb.WriteRune(placeholder)
			sb.WriteRune(rowColumnDiacritics[y])
			sb.WriteRune(rowColumnDiacritics[x])
		}
		sb.WriteString("\x1b[0m")
		lines = append(lines, sb.String())
	}
	return lines
}

// imageID derives the terminal-side id for one placement. It is a hash rather
// than a counter so the same image at the same size keeps its id across frames
// and needs transmitting only once. The id is 24 bits because that is what a
// foreground colour carries, and never zero, which the protocol reserves.
func imageID(path string, cols, rows int) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(path))
	_, _ = h.Write([]byte{byte(cols), byte(cols >> 8), byte(rows), byte(rows >> 8)})
	id := h.Sum32() & 0xFFFFFF
	if id == 0 {
		id = 1
	}
	return id
}

// ImageBounds returns the pixel size of the image at path, decoding only its
// header. It accepts every format the image package has registered, unlike
// ImageSize, which is limited to the two the graphics protocols carry.
func ImageBounds(path string) (int, int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width < 1 || cfg.Height < 1 {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}
