//go:build !unix

package media

// CellPixels reports no cell size away from Unix, where there is no winsize
// ioctl to ask. Callers fall back to assuming a cell twice as tall as it is
// wide, so an image is still placed, just at the usual proportion.
func CellPixels(uintptr) (w, h int, ok bool) { return 0, 0, false }
