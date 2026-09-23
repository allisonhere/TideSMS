package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Pictures being sent are copied into the app's cache before they go, so the
// message keeps its image when the original is moved or was only ever on the
// clipboard, and so a large photo can be shrunk without touching the original.

// outgoingDir holds prepared pictures. A variable so tests can redirect it.
var outgoingDir = func() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "tidesms-outgoing")
	}
	return filepath.Join(dir, "tidesms", "outgoing")
}

const (
	// maxSource refuses anything too big to be a picture worth texting.
	maxSource = 50 << 20
	// shrinkAbove is the size past which a photo is scaled down before it is
	// sent. KDE Connect carries the whole file, base64-encoded, in one network
	// message, and the phone shrinks it again to fit MMS anyway.
	shrinkAbove = 1500 << 10
	// maxUnshrunk is the most sent as it is when nothing can shrink it.
	maxUnshrunk = 8 << 20
	// sendEdge is the longest side of a shrunk photo.
	sendEdge = "1600x1600>"
)

// Outgoing is a picture ready to send.
type Outgoing struct {
	Path, Name, MIME string
	Size             int64
	Width, Height    int
}

// imageTypes are the formats offered and accepted, by extension.
var imageTypes = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif",
	".webp": "image/webp", ".heic": "image/heic", ".heif": "image/heif", ".bmp": "image/bmp",
}

// IsImage reports whether a file name is one of the picture formats that can
// be sent.
func IsImage(name string) bool {
	_, ok := imageTypes[strings.ToLower(filepath.Ext(name))]
	return ok
}

// sniff is the file's picture type from its content, or its extension for
// formats the standard library does not recognise (HEIC).
func sniff(head []byte, name string) (string, bool) {
	if t := http.DetectContentType(head); strings.HasPrefix(t, "image/") {
		return t, true
	}
	if t, ok := imageTypes[strings.ToLower(filepath.Ext(name))]; ok && (t == "image/heic" || t == "image/heif") {
		return t, true
	}
	return "", false
}

// Prepare checks that src is a picture and copies it, shrunk when large, into
// the outgoing cache. The original is never changed.
func Prepare(src string) (Outgoing, error) {
	info, err := os.Stat(src)
	if err != nil {
		return Outgoing{}, fmt.Errorf("could not open %s", filepath.Base(src))
	}
	if !info.Mode().IsRegular() {
		return Outgoing{}, fmt.Errorf("%s is not a file", filepath.Base(src))
	}
	if info.Size() == 0 || info.Size() > maxSource {
		return Outgoing{}, fmt.Errorf("%s is too large to send", filepath.Base(src))
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return Outgoing{}, fmt.Errorf("could not read %s", filepath.Base(src))
	}
	mime, ok := sniff(data[:min(len(data), 512)], src)
	if !ok {
		return Outgoing{}, fmt.Errorf("%s is not a picture", filepath.Base(src))
	}
	sum := sha256.Sum256(data)
	dir := filepath.Join(outgoingDir(), hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Outgoing{}, err
	}
	name := cleanName(filepath.Base(src), mime)
	// A GIF is sent as it is, so it stays animated; so is anything already
	// small in a format the phone reads.
	plain := mime == "image/jpeg" || mime == "image/png" || mime == "image/gif"
	if mime == "image/gif" || (plain && info.Size() <= shrinkAbove) {
		return write(filepath.Join(dir, name), data, mime)
	}
	if out, err := shrink(src, filepath.Join(dir, strings.TrimSuffix(name, filepath.Ext(name))+".jpg")); err == nil {
		return out, nil
	}
	if plain && info.Size() <= maxUnshrunk {
		return write(filepath.Join(dir, name), data, mime)
	}
	return Outgoing{}, fmt.Errorf("%s needs ImageMagick to be made small enough to send", filepath.Base(src))
}

// cleanName keeps the picture's own name, which the phone shows, without
// anything that could not be a plain file name.
func cleanName(name, mime string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == '/' || r == '\\' || r == 0x7f {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		name = "picture" + name
	}
	if filepath.Ext(name) == "" {
		for ext, t := range imageTypes {
			if t == mime && ext != ".jpeg" && ext != ".heif" {
				name += ext
				break
			}
		}
	}
	return name
}

func write(path string, data []byte, mime string) (Outgoing, error) {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Outgoing{}, err
	}
	return describe(path, mime)
}

// shrink writes a JPEG no larger than sendEdge, honouring the photo's
// orientation, with ImageMagick. HEIC and WebP go the same way, since the
// phone may not read them.
func shrink(src, out string) (Outgoing, error) {
	bin, err := exec.LookPath("magick")
	if err != nil {
		return Outgoing{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), convertTimeout)
	defer cancel()
	tmp := out + ".tmp.jpg"
	defer func() { _ = os.Remove(tmp) }()
	if b, err := exec.CommandContext(ctx, bin, src+"[0]", "-auto-orient", "-resize", sendEdge, "-quality", "85", "-strip", tmp).CombinedOutput(); err != nil {
		return Outgoing{}, fmt.Errorf("magick: %v %s", err, b)
	}
	if err := os.Rename(tmp, out); err != nil {
		return Outgoing{}, err
	}
	return describe(out, "image/jpeg")
}

func describe(path, mime string) (Outgoing, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Outgoing{}, err
	}
	o := Outgoing{Path: path, Name: filepath.Base(path), MIME: mime, Size: info.Size()}
	if f, err := os.Open(path); err == nil {
		if cfg, _, err := image.DecodeConfig(f); err == nil {
			o.Width, o.Height = cfg.Width, cfg.Height
		}
		_ = f.Close()
	}
	return o, nil
}

// clipboardTools are tried in order: Wayland first, then X11.
var clipboardTools = []struct {
	name  string
	types []string
	read  func(mime string) []string
}{
	{"wl-paste", []string{"--list-types"}, func(t string) []string { return []string{"--no-newline", "--type", t} }},
	{"xclip", []string{"-selection", "clipboard", "-t", "TARGETS", "-o"}, func(t string) []string { return []string{"-selection", "clipboard", "-t", t, "-o"} }},
}

// ErrNoClipboardImage means the clipboard holds no picture.
var ErrNoClipboardImage = errors.New("the clipboard has no picture")

// clipboardTypes lists what the clipboard offers, with the tool that said so.
func clipboardTypes(ctx context.Context) (string, []string) {
	for _, tool := range clipboardTools {
		if tool.name == "wl-paste" && os.Getenv("WAYLAND_DISPLAY") == "" {
			continue
		}
		if tool.name == "xclip" && os.Getenv("DISPLAY") == "" {
			continue
		}
		bin, err := exec.LookPath(tool.name)
		if err != nil {
			continue
		}
		out, err := exec.CommandContext(ctx, bin, tool.types...).Output()
		if err != nil {
			continue
		}
		return bin, strings.Fields(string(out))
	}
	return "", nil
}

// pictureType is the best picture format on offer, and whether the clipboard
// also holds text.
func pictureType(types []string) (picture string, text bool) {
	rank := map[string]int{"image/png": 3, "image/jpeg": 2, "image/gif": 1}
	best := -1
	for _, t := range types {
		switch {
		case strings.HasPrefix(t, "text/plain"), t == "UTF8_STRING", t == "STRING", t == "TEXT":
			text = true
		case strings.HasPrefix(t, "image/"):
			if r := rank[t]; r > best {
				best, picture = r, t
			}
		}
	}
	return picture, text
}

// ClipboardHasOnlyPicture reports whether the clipboard holds a picture and no
// text, which is when a paste should attach rather than type.
func ClipboardHasOnlyPicture(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, types := clipboardTypes(ctx)
	picture, text := pictureType(types)
	return picture != "" && !text
}

// ClipboardPicture saves the clipboard's picture and prepares it to send.
func ClipboardPicture(ctx context.Context) (Outgoing, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	bin, types := clipboardTypes(ctx)
	picture, _ := pictureType(types)
	if picture == "" {
		return Outgoing{}, ErrNoClipboardImage
	}
	var read []string
	for _, tool := range clipboardTools {
		if filepath.Base(bin) == tool.name {
			read = tool.read(picture)
		}
	}
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, read...)
	cmd.Stdout = &limited{w: &out, n: maxSource}
	if err := cmd.Run(); err != nil || out.Len() == 0 {
		return Outgoing{}, errors.New("could not read the picture from the clipboard")
	}
	if err := os.MkdirAll(outgoingDir(), 0o700); err != nil {
		return Outgoing{}, err
	}
	ext := ".png"
	for e, t := range imageTypes {
		if t == picture && e != ".jpeg" {
			ext = e
		}
	}
	tmp, err := os.CreateTemp(outgoingDir(), "pasted-*"+ext)
	if err != nil {
		return Outgoing{}, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(out.Bytes()); err != nil {
		_ = tmp.Close()
		return Outgoing{}, err
	}
	if err := tmp.Close(); err != nil {
		return Outgoing{}, err
	}
	o, err := Prepare(tmp.Name())
	if err != nil {
		return Outgoing{}, err
	}
	// The temporary name means nothing to the person receiving it.
	named := filepath.Join(filepath.Dir(o.Path), "pasted"+filepath.Ext(o.Path))
	if err := os.Rename(o.Path, named); err == nil {
		o.Path, o.Name = named, filepath.Base(named)
	}
	return o, nil
}

// limited stops a runaway clipboard from filling memory.
type limited struct {
	w io.Writer
	n int64
}

func (l *limited) Write(p []byte) (int, error) {
	if int64(len(p)) > l.n {
		return 0, errors.New("clipboard picture too large")
	}
	l.n -= int64(len(p))
	return l.w.Write(p)
}

// Candidate is a picture offered by the picker.
type Candidate struct {
	Path     string
	Size     int64
	Modified time.Time
}

// pictureDirs are where recent pictures are looked for, relative to home.
var pictureDirs = []string{"Pictures", "Downloads", "Desktop", "Documents"}

// RecentPictures lists pictures in the usual places under home, newest first.
// It looks a few folders deep and skips hidden ones, so it stays quick on a
// large home directory.
func RecentPictures(home string, limit int) []Candidate {
	var out []Candidate
	seen := map[string]bool{}
	for _, d := range pictureDirs {
		root := filepath.Join(home, d)
		_ = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if e.IsDir() {
				if path != root && (strings.HasPrefix(e.Name(), ".") || strings.Count(strings.TrimPrefix(path, root), string(filepath.Separator)) > 3) {
					return filepath.SkipDir
				}
				return nil
			}
			if !IsImage(e.Name()) || seen[path] {
				return nil
			}
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			seen[path] = true
			out = append(out, Candidate{Path: path, Size: info.Size(), Modified: info.ModTime()})
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Complete lists the entries of the folder a typed path points into whose
// names start with what has been typed after the last slash: folders, and
// pictures. It is what Tab completion in the picker offers.
func Complete(typed, home string) []Candidate {
	path := typed
	if strings.HasPrefix(path, "~") {
		path = home + path[1:]
	}
	dir, prefix := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Candidate
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !e.IsDir() && !IsImage(name) {
			continue
		}
		full := filepath.Join(dir, name)
		if e.IsDir() {
			full += string(filepath.Separator)
		}
		out = append(out, Candidate{Path: full, Size: info.Size(), Modified: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
