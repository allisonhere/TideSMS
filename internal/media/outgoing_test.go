package media

import (
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withOutgoing(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := outgoingDir
	outgoingDir = func() string { return dir }
	t.Cleanup(func() { outgoingDir = old })
	return dir
}

func writeTestPNG(t *testing.T, path string, w, h int, noisy bool) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewSource(1))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{uint8(x), uint8(y), 90, 255}
			if noisy {
				c = color.RGBA{uint8(r.Intn(256)), uint8(r.Intn(256)), uint8(r.Intn(256)), 255}
			}
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// A small picture is copied as it is, keeping its name, and the original is
// left alone.
func TestPrepareCopiesASmallPicture(t *testing.T) {
	cache := withOutgoing(t)
	src := filepath.Join(t.TempDir(), "sunset.png")
	writeTestPNG(t, src, 40, 30, false)
	before, _ := os.ReadFile(src)
	o, err := Prepare(src)
	if err != nil {
		t.Fatal(err)
	}
	if o.Name != "sunset.png" || o.MIME != "image/png" || o.Width != 40 || o.Height != 30 || !strings.HasPrefix(o.Path, cache) {
		t.Fatalf("prepared %+v", o)
	}
	after, _ := os.ReadFile(src)
	if string(before) != string(after) {
		t.Error("the original was changed")
	}
}

// A large photo is shrunk to a JPEG no wider than the send size.
func TestPrepareShrinksALargePhoto(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick not installed")
	}
	withOutgoing(t)
	src := filepath.Join(t.TempDir(), "big.png")
	writeTestPNG(t, src, 2400, 1800, true)
	if info, _ := os.Stat(src); info.Size() <= shrinkAbove {
		t.Fatalf("fixture too small to need shrinking: %d", info.Size())
	}
	o, err := Prepare(src)
	if err != nil {
		t.Fatal(err)
	}
	if o.MIME != "image/jpeg" || filepath.Ext(o.Path) != ".jpg" || o.Width > 1600 || o.Height > 1600 || o.Width == 0 {
		t.Fatalf("shrunk to %+v", o)
	}
}

// A GIF is sent as it is, so an animation survives.
func TestPrepareKeepsAGIF(t *testing.T) {
	withOutgoing(t)
	src := filepath.Join(t.TempDir(), "wave.gif")
	f, _ := os.Create(src)
	pal := image.NewPaletted(image.Rect(0, 0, 4, 4), []color.Color{color.Black, color.White})
	_ = gif.EncodeAll(f, &gif.GIF{Image: []*image.Paletted{pal, pal}, Delay: []int{10, 10}})
	_ = f.Close()
	o, err := Prepare(src)
	if err != nil || o.MIME != "image/gif" || o.Name != "wave.gif" {
		t.Fatalf("%+v %v", o, err)
	}
}

// Anything that is not a picture is refused, whatever it is called.
func TestPrepareRefusesNonPictures(t *testing.T) {
	withOutgoing(t)
	dir := t.TempDir()
	fake := filepath.Join(dir, "notes.png")
	_ = os.WriteFile(fake, []byte("just some text"), 0o600)
	if _, err := Prepare(fake); err == nil || !strings.Contains(err.Error(), "not a picture") {
		t.Errorf("text named .png: %v", err)
	}
	if _, err := Prepare(dir); err == nil {
		t.Error("a folder was accepted")
	}
	if _, err := Prepare(filepath.Join(dir, "missing.jpg")); err == nil {
		t.Error("a missing file was accepted")
	}
}

// A paste attaches only when the clipboard offers a picture and no text, and
// prefers PNG among several formats.
func TestPictureType(t *testing.T) {
	for _, tc := range []struct {
		types   []string
		picture string
		text    bool
	}{
		{[]string{"image/png"}, "image/png", false},
		{[]string{"image/jpeg", "image/png", "image/bmp"}, "image/png", false},
		{[]string{"text/html", "image/png"}, "image/png", false},
		{[]string{"text/plain;charset=utf-8", "UTF8_STRING", "image/png"}, "image/png", true},
		{[]string{"text/plain"}, "", true},
		{nil, "", false},
	} {
		picture, text := pictureType(tc.types)
		if picture != tc.picture || text != tc.text {
			t.Errorf("%v: got %q %v", tc.types, picture, text)
		}
	}
}

// Recent pictures come newest first, from the usual folders, skipping hidden
// folders and anything that is not a picture.
func TestRecentPictures(t *testing.T) {
	home := t.TempDir()
	mk := func(rel string, age time.Duration) {
		p := filepath.Join(home, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte("x"), 0o600)
		when := time.Now().Add(-age)
		_ = os.Chtimes(p, when, when)
	}
	mk("Pictures/old.jpg", 48*time.Hour)
	mk("Downloads/new.png", time.Minute)
	mk("Pictures/Screenshots/shot.png", time.Hour)
	mk("Pictures/.thumbnails/hidden.png", 0)
	mk("Documents/report.pdf", 0)
	mk("Music/cover.jpg", 0)
	got := RecentPictures(home, 10)
	var names []string
	for _, c := range got {
		names = append(names, filepath.Base(c.Path))
	}
	if strings.Join(names, ",") != "new.png,shot.png,old.jpg" {
		t.Fatalf("recent = %v", names)
	}
}

// Completion lists folders and pictures matching what was typed.
func TestComplete(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, "Pictures", "Trips"), 0o700)
	_ = os.WriteFile(filepath.Join(home, "Pictures", "trail.jpg"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(home, "Pictures", "todo.txt"), []byte("x"), 0o600)
	got := Complete("~/Pictures/t", home)
	var names []string
	for _, c := range got {
		names = append(names, strings.TrimPrefix(c.Path, filepath.Join(home, "Pictures")+"/"))
	}
	if strings.Join(names, ",") != "Trips/,trail.jpg" {
		t.Fatalf("complete = %v", names)
	}
}
