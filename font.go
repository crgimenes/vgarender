package vgarender

//go:generate go run ./internal/genfont -in internal/genfont/3dfx8x16.bin -out font3dfx.go

// GlyphWidth is the source pixel width of the VGA fonts. Text cells are one
// pixel wider (see CellWidth) to make room for the VGA "9-dot" column.
const GlyphWidth = 8

// Font is a bitmap text-mode font. Glyphs holds 256 glyphs; each glyph is
// Height bytes, one byte per scanline, with the most significant bit as the
// leftmost pixel. Glyphs may be mutated at run time to override individual
// characters (e.g. to mimic programs that rewrote the font ROM such as DOSShell).
type Font struct {
	Height int
	Glyphs [256][]byte
}

// Font3dfx returns a fresh copy of the built-in 3dfx 8x16 font. Each call
// allocates independent glyph storage, so callers may mutate the result without
// affecting the package's font data or other Font instances.
func Font3dfx() *Font {
	f := &Font{Height: 16}
	for c := range font3dfx {
		row := make([]byte, 16)
		copy(row, font3dfx[c][:])
		f.Glyphs[c] = row
	}
	return f
}

// glyphRow returns the nine horizontal pixels of glyph ch at scanline row. Bit
// (8-col) of the result is the pixel at column col (0 = leftmost). Column 8 is
// the VGA "9-dot" column: it is lit only for the line-drawing glyphs 0xC0..0xDF,
// where it mirrors column 7 so horizontal runs stay continuous between cells.
func glyphRow(f *Font, ch byte, row int) uint16 {
	if row < 0 || row >= f.Height {
		return 0
	}

	bits := uint16(f.Glyphs[ch][row]) // 8 source pixels, MSB = column 0
	line := bits << 1                 // columns 0..7 occupy result bits 8..1
	if ch >= 0xc0 && ch <= 0xdf && bits&0x01 != 0 {
		line |= 0x01 // column 8 mirrors column 7
	}
	return line
}
