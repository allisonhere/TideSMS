package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/allisonhere/tidesms/internal/media"
	"golang.org/x/term"
)

// runImageViewer draws one image on the real terminal and waits for a key. It
// runs in a child process with the TUI suspended, so nothing pads or rewrites
// the graphics escape. Kitty and iTerm2 get the real image; anything else gets
// a large braille rendering so the feature still works without graphics.
func runImageViewer(path string) error {
	fd := int(os.Stdout.Fd())
	cols, rows, err := term.GetSize(fd)
	if err != nil || cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	imgCols := max(20, cols-4)
	imgRows := max(6, rows-6)

	out := ""
	if p := media.Detect(os.Getenv); p == media.Kitty || p == media.ITerm {
		// Ask for the image's own shape at no more than its own resolution.
		// Given the whole screen the terminal stretches the image to fill it,
		// which shows a small attachment blown up and soft rather than as it is.
		if w, h, ok := media.ImageBounds(path); ok {
			cw, ch, _ := media.CellPixels(uintptr(fd))
			if bc, br := media.ViewerBox(w, h, imgCols, imgRows, cw, ch); bc > 0 && br > 0 {
				imgCols, imgRows = bc, br
			}
		}
		if seq, ok := media.Render(p, path, imgCols, imgRows); ok {
			out = seq
		}
	}
	if out == "" {
		lines, ok := media.BrailleImage(path, imgCols, imgRows)
		if !ok {
			return fmt.Errorf("cannot display %s", filepath.Base(path))
		}
		out = strings.Join(lines, "\n")
	}

	clear := "\x1b[2J\x1b[H"
	fmt.Print(clear)
	fmt.Println(out)
	fmt.Printf("\n%s — press any key to return", filepath.Base(path))

	if old, rawErr := term.MakeRaw(fd); rawErr == nil {
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
		_ = term.Restore(fd, old)
	} else {
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	// Delete any kitty image and clear before the TUI repaints.
	fmt.Print(clear + "\x1b_Ga=d\x1b\\")
	return nil
}
