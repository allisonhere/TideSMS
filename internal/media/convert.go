package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Phones send photos in formats Go cannot decode, most often HEIC, the default
// camera format on current iPhones and Samsungs. Such a part is converted once
// to a PNG beside the app's cache and drawn from there; the original is left
// where the backend put it, since that is the file the user owns and saves.

// convertedDir is where converted copies live. It is a variable so tests can
// use a temporary directory.
var convertedDir = func() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "tidesms-converted")
	}
	return filepath.Join(dir, "tidesms", "converted")
}

// converters are tried in order. ImageMagick resizes as it converts, which
// keeps a 5 MB phone photo from becoming a 36 MB PNG, and honours the photo's
// orientation; heif-convert covers a system with libheif but no ImageMagick.
var converters = []struct {
	name string
	args func(in, out string) []string
}{
	{"magick", func(in, out string) []string {
		return []string{in + "[0]", "-auto-orient", "-resize", "2048x2048>", out}
	}},
	{"heif-convert", func(in, out string) []string { return []string{in, out} }},
}

// convertTimeout bounds one conversion; a large photo takes about a second.
const convertTimeout = 30 * time.Second

// Readable reports whether the app can decode an image file itself.
func Readable(path string) bool {
	if path == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	return err == nil && cfg.Width > 0 && cfg.Height > 0
}

// Displayable returns a file the app can draw for path: path itself when it
// can decode it, or a copy already converted from it. It never converts, so it
// is cheap enough for rendering; Convert does the work, off the render path.
func Displayable(path string) (string, bool) {
	if Readable(path) {
		return path, true
	}
	out, err := convertedPath(path)
	if err != nil {
		return "", false
	}
	if Readable(out) {
		return out, true
	}
	return "", false
}

// NeedsConversion reports whether path is a file the app cannot draw and has
// not converted yet.
func NeedsConversion(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	_, ok := Displayable(path)
	return !ok
}

// Convert makes a drawable PNG copy of path and returns it. A path the app can
// already decode is returned as it is.
func Convert(path string) (string, error) {
	if p, ok := Displayable(path); ok {
		return p, nil
	}
	out, err := convertedPath(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return "", err
	}
	// Write beside the destination and rename, so a half-written file is never
	// mistaken for a finished one by a render that happens meanwhile.
	tmp := out + ".tmp.png"
	defer func() { _ = os.Remove(tmp) }()
	var errs []error
	for _, c := range converters {
		bin, err := exec.LookPath(c.name)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), convertTimeout)
		output, err := exec.CommandContext(ctx, bin, c.args(path, tmp)...).CombinedOutput()
		cancel()
		if err != nil || !Readable(tmp) {
			errs = append(errs, fmt.Errorf("%s: %v %s", c.name, err, output))
			continue
		}
		if err := os.Rename(tmp, out); err != nil {
			return "", err
		}
		return out, nil
	}
	if len(errs) == 0 {
		return "", errors.New("no image converter installed (ImageMagick or heif-convert)")
	}
	return "", errors.Join(errs...)
}

// convertedPath names the copy for a file by its identity, so a part that is
// replaced gets a fresh conversion rather than a stale one.
func convertedPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	key := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", abs, info.Size(), info.ModTime().UnixNano())))
	return filepath.Join(convertedDir(), hex.EncodeToString(key[:16])+".png"), nil
}
