//go:build unix

package media

import "golang.org/x/sys/unix"

// CellPixels reports the terminal's cell size in pixels, which is what an image
// needs to be given a height that matches its own proportions. Terminals are
// not obliged to report it — a multiplexer usually does not — so a false return
// is ordinary and the caller falls back to assuming a cell twice as tall as it
// is wide.
func CellPixels(fd uintptr) (w, h int, ok bool) {
	ws, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
		return 0, 0, false
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row), true
}
