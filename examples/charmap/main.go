// Command charmap renders the full 256-glyph 3dfx VGA font as a 16x16 map and
// draws a few boxes, exercising the 9-dot line-drawing behaviour and colours.
package main

import (
	"fmt"
	"log"

	"github.com/crgimenes/vgarender"
)

// attr builds a VGA attribute byte from foreground and background indices.
func attr(fg, bg byte) byte { return (bg&0x0f)<<4 | fg&0x0f }

// boxStyle holds the six CP437 code points used to draw a rectangle.
type boxStyle struct{ tl, tr, bl, br, hz, vt byte }

var (
	doubleBox = boxStyle{tl: 0xc9, tr: 0xbb, bl: 0xc8, br: 0xbc, hz: 0xcd, vt: 0xba} // ╔ ╗ ╚ ╝ ═ ║
	singleBox = boxStyle{tl: 0xda, tr: 0xbf, bl: 0xc0, br: 0xd9, hz: 0xc4, vt: 0xb3} // ┌ ┐ └ ┘ ─ │
)

func drawBox(t *vgarender.Text, x, y, w, h int, a byte, s boxStyle) {
	t.SetCell(x, y, s.tl, a)
	t.SetCell(x+w-1, y, s.tr, a)
	t.SetCell(x, y+h-1, s.bl, a)
	t.SetCell(x+w-1, y+h-1, s.br, a)
	for i := 1; i < w-1; i++ {
		t.SetCell(x+i, y, s.hz, a)
		t.SetCell(x+i, y+h-1, s.hz, a)
	}
	for i := 1; i < h-1; i++ {
		t.SetCell(x, y+i, s.vt, a)
		t.SetCell(x+w-1, y+i, s.vt, a)
	}
}

func main() {
	t := vgarender.NewText(80, 25, vgarender.WithScale(2))
	t.Clear(attr(7, 0))
	t.ShowCursor(false)

	t.Print(2, 0, "vgarender - 3dfx 8x16 VGA text mode (80x25, 720x400)", attr(15, 0))

	// 16x16 character map with hex row/column headers.
	const ox, oy = 6, 3
	for i := range 16 {
		t.Print(ox+i, oy-1, fmt.Sprintf("%X", i), attr(11, 0))
		t.Print(ox-3, oy+i, fmt.Sprintf("%X_", i), attr(11, 0))
	}
	for c := range 256 {
		t.SetCell(ox+c%16, oy+c/16, byte(c), attr(14, 0))
	}

	// Double box: shows continuous horizontal lines via the 9-dot column.
	drawBox(t, 30, 3, 28, 6, attr(11, 1), doubleBox)
	t.Print(32, 4, "Double box (0xC9..0xBC)", attr(15, 1))
	t.Print(32, 5, "9-dot keeps lines joined", attr(14, 1))
	t.Print(32, 7, "light cyan on blue", attr(11, 1))

	// Single box with a blinking label.
	drawBox(t, 30, 10, 28, 5, attr(10, 0), singleBox)
	t.Print(32, 11, "Single box (0xDA..0xD9)", attr(15, 0))
	t.Print(32, 12, "Blink:", attr(7, 0))
	t.Print(39, 12, "BLINKING", 0x80|attr(14, 0))
	t.Print(32, 13, "brown 6:", attr(7, 0))
	t.Print(41, 13, "####", attr(6, 0))

	// A short palette strip (background colours 0..15).
	t.Print(6, 21, "Palette:", attr(15, 0))
	for i := range 16 {
		t.SetCell(15+i, 21, ' ', attr(0, byte(i)))
	}

	t.Print(6, 23, "F11 or Alt+Enter: toggle fullscreen", attr(8, 0))

	if err := t.Run("vgarender - charmap"); err != nil {
		log.Fatal(err)
	}
}
