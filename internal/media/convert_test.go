package media

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writePNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for x := 0; x < 40; x++ {
		img.Set(x, 10, color.RGBA{255, 0, 0, 255})
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func useConvertedDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	previous := convertedDir
	convertedDir = func() string { return dir }
	t.Cleanup(func() { convertedDir = previous })
}

// A file the app decodes itself is drawn as it is and never converted.
func TestReadableImagesNeedNoConversion(t *testing.T) {
	useConvertedDir(t)
	path := filepath.Join(t.TempDir(), "a.png")
	writePNG(t, path)
	if got, ok := Displayable(path); !ok || got != path {
		t.Fatalf("Displayable = %q %v", got, ok)
	}
	if NeedsConversion(path) {
		t.Error("a PNG was marked for conversion")
	}
}

// A HEIC photo, the format current phones send, is converted once to a PNG
// the app can draw; until then it is reported as needing it.
func TestHEICIsConvertedToAReadableCopy(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick not installed")
	}
	useConvertedDir(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writePNG(t, src)
	heic := filepath.Join(dir, "PART_1") // parts arrive without an extension
	if out, err := exec.Command("magick", src, "heic:"+heic).CombinedOutput(); err != nil {
		t.Skipf("ImageMagick cannot write HEIC here: %v %s", err, out)
	}
	if Readable(heic) {
		t.Fatal("test premise: Go should not decode HEIC")
	}
	if !NeedsConversion(heic) {
		t.Fatal("HEIC not marked for conversion")
	}
	if _, ok := Displayable(heic); ok {
		t.Fatal("Displayable claimed a copy before converting")
	}
	out, err := Convert(heic)
	if err != nil {
		t.Fatal(err)
	}
	if !Readable(out) {
		t.Fatalf("converted copy unreadable: %s", out)
	}
	if got, ok := Displayable(heic); !ok || got != out {
		t.Errorf("Displayable after conversion = %q %v, want %q", got, ok, out)
	}
	if NeedsConversion(heic) {
		t.Error("still marked for conversion after converting")
	}
	if Readable(heic) != false {
		t.Error("the original was modified")
	}
}

// Without a converter the failure is reported rather than leaving a broken
// file behind.
func TestConvertWithoutAConverterFails(t *testing.T) {
	useConvertedDir(t)
	previous := converters
	converters = nil
	t.Cleanup(func() { converters = previous })
	path := filepath.Join(t.TempDir(), "PART_2")
	if err := os.WriteFile(path, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Convert(path); err == nil {
		t.Fatal("conversion claimed success with no converter")
	}
	if _, ok := Displayable(path); ok {
		t.Error("an unconvertible file was reported displayable")
	}
}
