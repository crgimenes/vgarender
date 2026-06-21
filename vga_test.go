package vgarender

import (
	"image/color"
	"strings"
	"testing"
)

func TestGlyphRowNinthColumn(t *testing.T) {
	f := &Font{Height: 16}
	for c := range f.Glyphs {
		f.Glyphs[c] = make([]byte, 16)
	}

	tests := []struct {
		name string
		ch   byte
		bits byte
		want uint16
	}{
		{"line char replicates col7 into col8", 0xc4, 0x01, 0b000000011},
		{"line char with col7 clear leaves col8 clear", 0xc4, 0x80, 0b100000000},
		{"non-line char never lights col8", 0x41, 0x01, 0b000000010},
		{"full line glyph fills all nine columns", 0xdb, 0xff, 0b111111111},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f.Glyphs[tc.ch][0] = tc.bits
			got := glyphRow(f, tc.ch, 0)
			if got != tc.want {
				t.Fatalf("glyphRow(%#x, bits=%#x) = %09b, want %09b", tc.ch, tc.bits, got, tc.want)
			}
		})
	}
}

func TestGlyphRowOutOfRange(t *testing.T) {
	f := Font3dfx()
	if got := glyphRow(f, 0x41, -1); got != 0 {
		t.Fatalf("row -1 = %#b, want 0", got)
	}
	if got := glyphRow(f, 0x41, f.Height); got != 0 {
		t.Fatalf("row %d = %#b, want 0", f.Height, got)
	}
}

func TestFont3dfx(t *testing.T) {
	f := Font3dfx()
	if f.Height != 16 {
		t.Fatalf("height = %d, want 16", f.Height)
	}

	for r := range 16 {
		if f.Glyphs[0x20][r] != 0 {
			t.Fatalf("space glyph row %d is not blank: %#x", r, f.Glyphs[0x20][r])
		}
	}

	lit := false
	for r := range 16 {
		if f.Glyphs[0x41][r] != 0 {
			lit = true
		}
	}
	if !lit {
		t.Fatal("glyph 'A' is blank")
	}

	g := Font3dfx()
	orig := f.Glyphs[0x41][0]
	g.Glyphs[0x41][0] ^= 0xff
	if f.Glyphs[0x41][0] != orig {
		t.Fatal("Font3dfx instances share glyph storage")
	}
}

func TestAttrColors(t *testing.T) {
	scr := NewText(1, 1)

	scr.SetBlinkEnabled(true)
	fg, bg := scr.attrColors(0x1f, true) // fg 15 white, bg 1 blue
	if fg != palette[15] || bg != palette[1] {
		t.Fatalf("attr 0x1f -> fg=%v bg=%v", fg, bg)
	}

	fgOff, bgOff := scr.attrColors(0x80|0x1f, false) // blink bit, off phase
	if fgOff != bgOff {
		t.Fatalf("blink off phase should hide glyph: fg=%v bg=%v", fgOff, bgOff)
	}

	fgOn, _ := scr.attrColors(0x80|0x1f, true) // blink bit, on phase
	if fgOn != palette[15] {
		t.Fatalf("blink on phase fg=%v, want white", fgOn)
	}

	scr.SetBlinkEnabled(false)
	_, bg2 := scr.attrColors(0x90, true) // bg bits 1001 = index 9
	if bg2 != palette[9] {
		t.Fatalf("blink-disabled bg=%v, want %v", bg2, palette[9])
	}
}

func TestMemRoundTrip(t *testing.T) {
	scr := NewText(80, 25)
	if got, want := len(scr.Mem()), 80*25*2; got != want {
		t.Fatalf("mem length = %d, want %d", got, want)
	}

	scr.SetCell(3, 4, 'A', 0x1f)
	code, attr := scr.Cell(3, 4)
	if code != 'A' || attr != 0x1f {
		t.Fatalf("Cell(3,4) = %q,%#x", code, attr)
	}

	scr.SetCell(-1, 0, 'X', 0xff)
	scr.SetCell(80, 0, 'X', 0xff)

	scr.PutChar(3, 4, 'B')
	code, attr = scr.Cell(3, 4)
	if code != 'B' || attr != 0x1f {
		t.Fatalf("PutChar changed attribute: %q,%#x", code, attr)
	}
}

func TestRenderFramebuffer(t *testing.T) {
	scr := NewText(2, 1)
	scr.ShowCursor(false)

	// Glyph 0xDB is in the 9-dot range; a full-block pattern lights all nine
	// columns (column 8 mirrors column 7).
	for r := range scr.font.Glyphs[0xdb] {
		scr.font.Glyphs[0xdb][r] = 0xff
	}
	scr.SetCell(0, 0, 0xdb, 0x0f) // white on black
	scr.SetCell(1, 0, ' ', 0x0f)  // blank -> all background (black)

	scr.render()

	at := func(x, y int) color.RGBA {
		o := (y*scr.cols*CellWidth + x) * 4
		return color.RGBA{scr.fb[o], scr.fb[o+1], scr.fb[o+2], scr.fb[o+3]}
	}
	for y := range CellHeight {
		for x := range CellWidth { // cell 0: every column white, including the 9th
			if at(x, y) != palette[15] {
				t.Fatalf("block cell pixel (%d,%d) = %v, want white", x, y, at(x, y))
			}
		}
		for x := CellWidth; x < 2*CellWidth; x++ { // cell 1: every column black
			if at(x, y) != palette[0] {
				t.Fatalf("blank cell pixel (%d,%d) = %v, want black", x, y, at(x, y))
			}
		}
	}
}

func TestSnapshot(t *testing.T) {
	scr := NewText(4, 2)
	img := scr.Snapshot()
	if got, want := img.Bounds().Dx(), 4*CellWidth; got != want {
		t.Fatalf("snapshot width = %d, want %d", got, want)
	}
	if got, want := img.Bounds().Dy(), 2*CellHeight; got != want {
		t.Fatalf("snapshot height = %d, want %d", got, want)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a == 0 {
		t.Fatal("snapshot pixel is not opaque")
	}
}

func TestPrintClipsAtRowEdge(t *testing.T) {
	scr := NewText(5, 1)
	scr.Print(3, 0, "ABCDE", 0x07)

	var got strings.Builder
	for col := range 5 {
		c, _ := scr.Cell(col, 0)
		got.WriteString(string(rune(c)))
	}
	if got.String() != "   AB" {
		t.Fatalf("clipped print = %q, want %q", got.String(), "   AB")
	}
}
