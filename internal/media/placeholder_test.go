package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// sizedPNG writes a PNG of the given pixel size, so a test can ask for a
// specific aspect ratio.
func sizedPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sized.png")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPlaceholdersOnlyKitty(t *testing.T) {
	// iTerm2 draws over the terminal rather than into the grid, so it cannot be
	// used inside a pane even though Render supports it for the viewer.
	for p, want := range map[Protocol]bool{Kitty: true, ITerm: false, Sixel: false, None: false} {
		if got := Placeholders(p); got != want {
			t.Errorf("Placeholders(%v) = %v, want %v", p, got, want)
		}
	}
}

func TestPlacementRowsFollowsAspect(t *testing.T) {
	// A square image in a cell twice as tall as it is wide takes half as many
	// rows as columns.
	if got := PlacementRows(100, 100, 20, 1, 2); got != 10 {
		t.Errorf("square in 1x2 cells: got %d rows, want 10", got)
	}
	// The same image in square cells takes as many rows as columns.
	if got := PlacementRows(100, 100, 20, 10, 10); got != 20 {
		t.Errorf("square in square cells: got %d rows, want 20", got)
	}
	// An unknown cell size falls back to the usual 1:2 proportion.
	if got, want := PlacementRows(100, 100, 20, 0, 0), PlacementRows(100, 100, 20, 1, 2); got != want {
		t.Errorf("unknown cell size: got %d rows, want %d", got, want)
	}
	// An image far wider than it is tall still occupies at least one row.
	if got := PlacementRows(1000, 1, 4, 7, 16); got < 1 {
		t.Errorf("very wide image: got %d rows, want at least 1", got)
	}
}

func TestPlacementBoxKeepsAspectWithinLimits(t *testing.T) {
	// A tall image that does not fit the row limit gives back width rather than
	// being squeezed: the box it gets must stay close to the image's own shape.
	cols, rows := PlacementBox(390, 466, 24, 8, 7, 16)
	if rows != 8 {
		t.Fatalf("rows = %d, want the cap of 8", rows)
	}
	if cols >= 24 {
		t.Errorf("cols = %d, want fewer than the 24 available so the aspect is kept", cols)
	}
	boxAspect := float64(cols*7) / float64(rows*16)
	imgAspect := 390.0 / 466.0
	if boxAspect < imgAspect*0.8 || boxAspect > imgAspect*1.25 {
		t.Errorf("box aspect %.3f is far from the image's %.3f", boxAspect, imgAspect)
	}
	// An image that fits keeps the full width.
	if cols, rows := PlacementBox(390, 466, 24, 20, 7, 16); cols != 24 || rows < 1 || rows > 20 {
		t.Errorf("fitting image: got %dx%d, want 24 columns and a row count within the cap", cols, rows)
	}
	// A degenerate request is refused rather than guessed at.
	if cols, rows := PlacementBox(0, 0, 24, 20, 7, 16); cols != 0 || rows != 0 {
		t.Errorf("empty image: got %dx%d, want 0x0", cols, rows)
	}
}

// TestInlineImageCellsAreOneColumnWide is the invariant the whole approach
// rests on: a placement is ordinary text as far as the layout is concerned, so
// a pane pads, truncates and aligns it exactly as it does a line of letters. If
// a placeholder cell ever measured as anything but one column, every bubble
// holding an image would be mismeasured.
func TestInlineImageCellsAreOneColumnWide(t *testing.T) {
	path := sizedPNG(t, 40, 20)
	_, lines, ok := InlineImage(path, 12, 5)
	if !ok {
		t.Fatal("InlineImage reported failure for a readable PNG")
	}
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 12 {
			t.Errorf("line %d measures %d columns, want 12", i, w)
		}
	}
}

func TestInlineImageEncodesRowsColumnsAndID(t *testing.T) {
	path := sizedPNG(t, 40, 20)
	transmit, lines, ok := InlineImage(path, 3, 2)
	if !ok {
		t.Fatal("InlineImage reported failure for a readable PNG")
	}
	// A PNG already on disk is sent by path, which keeps the escape small
	// however large the image is.
	if !strings.Contains(transmit, ",t=f;") {
		t.Errorf("transmission does not send the file by path: %q", transmit)
	}
	if !strings.HasPrefix(transmit, "\x1b_Ga=T,U=1,i=") || !strings.HasSuffix(transmit, "\x1b\\") {
		t.Errorf("transmission is not a virtual-placement APC: %q", transmit)
	}
	id := imageID(path, 3, 2)
	if !strings.Contains(transmit, ",i="+strconv.Itoa(int(id))+",") {
		t.Errorf("transmission does not carry image id %d: %q", id, transmit)
	}
	// The id travels in the foreground colour so every cell can be attributed
	// to its image without repeating it per cell.
	wantColor := "\x1b[38;2;" + strconv.Itoa(int((id>>16)&0xFF)) + ";" + strconv.Itoa(int((id>>8)&0xFF)) + ";" + strconv.Itoa(int(id&0xFF)) + "m"
	for i, line := range lines {
		if !strings.HasPrefix(line, wantColor) {
			t.Fatalf("line %d does not open with the id colour %q: %q", i, wantColor, line)
		}
	}
	// Each cell names its own row and column.
	for row, line := range lines {
		cells := []rune(strings.TrimSuffix(strings.TrimPrefix(line, wantColor), "\x1b[0m"))
		if len(cells) != 3*3 {
			t.Fatalf("row %d has %d runes, want 3 cells of 3 runes", row, len(cells))
		}
		for col := 0; col < 3; col++ {
			c := cells[col*3 : col*3+3]
			if c[0] != placeholder {
				t.Errorf("row %d col %d is not a placeholder rune", row, col)
			}
			if c[1] != rowColumnDiacritics[row] {
				t.Errorf("row %d col %d encodes the wrong row", row, col)
			}
			if c[2] != rowColumnDiacritics[col] {
				t.Errorf("row %d col %d encodes the wrong column", row, col)
			}
		}
	}
}

func TestInlineImageSendsNonPNGInline(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pic.jpg")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	transmit, _, ok := InlineImage(path, 4, 2)
	if !ok {
		t.Fatal("InlineImage reported failure for a readable JPEG")
	}
	// The terminal has no PNG on disk to read, so the converted image travels
	// in the escape itself.
	if strings.Contains(transmit, "t=f") {
		t.Errorf("a JPEG was sent by path, which the terminal cannot decode: %q", transmit)
	}
	if !strings.Contains(transmit, "f=100") {
		t.Errorf("transmission does not declare PNG data: %q", transmit)
	}
}

func TestInlineImageRefusesWhatItCannotDraw(t *testing.T) {
	path := sizedPNG(t, 40, 20)
	if _, _, ok := InlineImage(path, 0, 4); ok {
		t.Error("accepted zero columns")
	}
	// A placeholder can only name as many rows and columns as there are
	// diacritics; beyond that the caller must fall back to text.
	if _, _, ok := InlineImage(path, maxPlaceholderCells+1, 4); ok {
		t.Error("accepted more columns than a placeholder can address")
	}
	if _, _, ok := InlineImage(filepath.Join(t.TempDir(), "missing.png"), 4, 4); ok {
		t.Error("accepted a file that does not exist")
	}
}

func TestImageIDIsStableAndNeverZero(t *testing.T) {
	path := sizedPNG(t, 40, 20)
	// A stable id is what lets a frame reuse an image the terminal already
	// holds instead of retransmitting it.
	if imageID(path, 10, 5) != imageID(path, 10, 5) {
		t.Error("the same image at the same size changed id between calls")
	}
	if imageID(path, 10, 5) == imageID(path, 11, 5) {
		t.Error("a different size reused the same id, so the terminal would keep the old scaling")
	}
	if id := imageID(path, 10, 5); id == 0 || id > 0xFFFFFF {
		t.Errorf("id %d is outside the range a foreground colour carries", id)
	}
}

func TestImageBounds(t *testing.T) {
	if w, h, ok := ImageBounds(sizedPNG(t, 40, 20)); !ok || w != 40 || h != 20 {
		t.Errorf("got %dx%d ok=%v, want 40x20", w, h, ok)
	}
	if _, _, ok := ImageBounds(filepath.Join(t.TempDir(), "missing.png")); ok {
		t.Error("reported bounds for a file that does not exist")
	}
}

// A full-screen view shows the image, not a blown-up copy of it: given more
// room than the image has pixels, it asks only for the cells the image
// actually covers.
func TestViewerBoxNeverUpscales(t *testing.T) {
	// 390x466 in 7x16 cells covers 56x30 cells; a 171x34 screen has room to
	// spare, so the box must stay at the image's own size.
	cols, rows := ViewerBox(390, 466, 171, 34, 7, 16)
	if cols > 56 || rows > 30 {
		t.Errorf("got %dx%d cells, want no more than the image's own 56x30", cols, rows)
	}
	if cols < 50 || rows < 26 {
		t.Errorf("got %dx%d cells, want close to the image's own 56x30", cols, rows)
	}
	// A large photo is scaled down to fit, which is the one case where
	// resampling is the honest answer.
	cols, rows = ViewerBox(4000, 3000, 171, 34, 7, 16)
	if cols > 171 || rows > 34 {
		t.Errorf("got %dx%d cells, want to fit within 171x34", cols, rows)
	}
	if rows != 34 {
		t.Errorf("rows = %d, want the full 34 available to a photo that large", rows)
	}
	// Without a cell size there is no way to know the image's footprint, so the
	// full space is used rather than a guess.
	if cols, rows := ViewerBox(390, 466, 171, 34, 0, 0); cols != 171 || rows != 34 {
		t.Errorf("unknown cell size: got %dx%d, want the full 171x34", cols, rows)
	}
	if cols, rows := ViewerBox(0, 0, 171, 34, 7, 16); cols != 0 || rows != 0 {
		t.Errorf("empty image: got %dx%d, want 0x0", cols, rows)
	}
}

// The box a viewer asks for keeps the image's proportions, so the terminal has
// no reason to letterbox or stretch it.
func TestViewerBoxKeepsAspect(t *testing.T) {
	for _, tc := range []struct{ w, h int }{{390, 466}, {4000, 3000}, {1000, 200}, {200, 1000}} {
		cols, rows := ViewerBox(tc.w, tc.h, 171, 34, 7, 16)
		if cols < 1 || rows < 1 {
			t.Fatalf("%dx%d: got an empty box", tc.w, tc.h)
		}
		box := float64(cols*7) / float64(rows*16)
		img := float64(tc.w) / float64(tc.h)
		if box < img*0.75 || box > img*1.33 {
			t.Errorf("%dx%d: box aspect %.3f is far from the image's %.3f", tc.w, tc.h, box, img)
		}
	}
}
