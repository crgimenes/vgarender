package vgarender

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Cell geometry of the VGA 80x25 text mode displayed at 720x400.
const (
	CellWidth  = 9
	CellHeight = 16
)

// Blink half-cycle lengths, in frames at the default 60 TPS (~1.9 Hz).
const (
	attrBlinkPeriod   = 16
	cursorBlinkPeriod = 16
)

// Text is a VGA text-mode screen. It owns a faithful character/attribute buffer
// (two bytes per cell, like VGA memory at 0xB8000) and renders it with
// Ebitengine. It implements ebiten.Game.
type Text struct {
	cols, rows int
	scale      int    // initial window scale; the on-screen scale is computed per frame
	mem        []byte // cols*rows*2: [code, attr] per cell
	font       *Font
	blinkAttr  bool // true: attribute bit 7 means blink; false: background intensity

	fsKeys    bool         // handle the built-in fullscreen hotkeys (F11, Alt+Enter)
	startFull bool         // start in fullscreen
	update    func() error // per-frame callback invoked by Update; nil to disable

	cur    cursor
	ticks  uint64
	fb     []byte        // RGBA framebuffer at native resolution
	screen *ebiten.Image // offscreen native-resolution image, created lazily
}

type cursor struct {
	col, row   int
	start, end int
	visible    bool
}

// Option configures a Text screen.
type Option func(*Text)

// WithScale sets the initial window scale (default 2). The on-screen scale is
// recomputed each frame as the largest integer that fits, so this only sizes the
// opening window. Values below 1 are ignored.
func WithScale(s int) Option {
	return func(t *Text) {
		if s >= 1 {
			t.scale = s
		}
	}
}

// WithFont sets the font (default Font3dfx). A nil font is ignored.
func WithFont(f *Font) Option {
	return func(t *Text) {
		if f != nil {
			t.font = f
		}
	}
}

// WithFullscreen starts the screen in fullscreen.
func WithFullscreen(enabled bool) Option {
	return func(t *Text) { t.startFull = enabled }
}

// WithFullscreenKeys enables or disables the built-in fullscreen hotkeys (F11
// and Alt+Enter). It is enabled by default.
func WithFullscreenKeys(enabled bool) Option {
	return func(t *Text) { t.fsKeys = enabled }
}

// NewText creates a text screen of cols x rows cells filled with spaces on a
// light-gray-on-black attribute.
func NewText(cols, rows int, opts ...Option) *Text {
	t := &Text{
		cols:      cols,
		rows:      rows,
		scale:     2,
		mem:       make([]byte, cols*rows*2),
		font:      Font3dfx(),
		blinkAttr: true,
		fsKeys:    true,
		cur:       cursor{start: 14, end: 15, visible: true},
	}
	for _, o := range opts {
		o(t)
	}
	t.fb = make([]byte, cols*CellWidth*rows*CellHeight*4)
	t.Fill(' ', 0x07)
	return t
}

// Size returns the screen size in cells.
func (t *Text) Size() (cols, rows int) { return t.cols, t.rows }

// Mem returns the raw character/attribute buffer (two bytes per cell, like VGA
// memory at 0xB8000). Callers may read and write it directly.
func (t *Text) Mem() []byte { return t.mem }

func (t *Text) inBounds(col, row int) bool {
	return col >= 0 && col < t.cols && row >= 0 && row < t.rows
}

// SetCell writes the code point and attribute of a cell. Out-of-range positions
// are ignored.
func (t *Text) SetCell(col, row int, code, attr byte) {
	if !t.inBounds(col, row) {
		return
	}
	i := (row*t.cols + col) * 2
	t.mem[i] = code
	t.mem[i+1] = attr
}

// Cell returns the code point and attribute at a cell. Out-of-range positions
// return zero values.
func (t *Text) Cell(col, row int) (code, attr byte) {
	if !t.inBounds(col, row) {
		return 0, 0
	}
	i := (row*t.cols + col) * 2
	return t.mem[i], t.mem[i+1]
}

// PutChar writes a code point, keeping the cell's existing attribute.
func (t *Text) PutChar(col, row int, code byte) {
	if !t.inBounds(col, row) {
		return
	}
	t.mem[(row*t.cols+col)*2] = code
}

// Print writes the bytes of s starting at col,row using attr. Each byte is a
// CP437 code point. Writing stops at the right edge of the row (no wrap).
func (t *Text) Print(col, row int, s string, attr byte) {
	for i := 0; i < len(s) && col+i < t.cols; i++ {
		t.SetCell(col+i, row, s[i], attr)
	}
}

// Fill sets every cell to code with attribute attr.
func (t *Text) Fill(code, attr byte) {
	for i := 0; i < len(t.mem); i += 2 {
		t.mem[i] = code
		t.mem[i+1] = attr
	}
}

// Clear fills the screen with spaces using attr.
func (t *Text) Clear(attr byte) { t.Fill(' ', attr) }

// SetCursor moves the text cursor to col,row.
func (t *Text) SetCursor(col, row int) {
	t.cur.col = col
	t.cur.row = row
}

// ShowCursor sets cursor visibility.
func (t *Text) ShowCursor(visible bool) { t.cur.visible = visible }

// SetCursorShape sets the cursor's scanline range within a cell (0..CellHeight-1).
func (t *Text) SetCursorShape(start, end int) {
	t.cur.start = start
	t.cur.end = end
}

// SetBlinkEnabled selects the meaning of attribute bit 7: blink (true, the VGA
// default) or background intensity (false).
func (t *Text) SetBlinkEnabled(enabled bool) { t.blinkAttr = enabled }

// Font returns the active font. Mutate Font.Glyphs to override glyphs at run time.
func (t *Text) Font() *Font { return t.font }

// SetFont replaces the active font. A nil font is ignored.
func (t *Text) SetFont(f *Font) {
	if f != nil {
		t.font = f
	}
}

// Run opens a resizable window sized for the initial scale and runs the screen
// until the window is closed.
func (t *Text) Run(title string) error {
	ebiten.SetWindowSize(t.cols*CellWidth*t.scale, t.rows*CellHeight*t.scale)
	ebiten.SetWindowTitle(title)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowSizeLimits(t.cols*CellWidth, t.rows*CellHeight, -1, -1)
	if t.startFull {
		ebiten.SetFullscreen(true)
	}
	return ebiten.RunGame(t)
}

// SetFullscreen enables or disables fullscreen.
func (t *Text) SetFullscreen(enabled bool) { ebiten.SetFullscreen(enabled) }

// ToggleFullscreen flips between windowed and fullscreen.
func (t *Text) ToggleFullscreen() { ebiten.SetFullscreen(!ebiten.IsFullscreen()) }

// IsFullscreen reports whether the screen is fullscreen.
func (t *Text) IsFullscreen() bool { return ebiten.IsFullscreen() }

func altPressed() bool {
	return ebiten.IsKeyPressed(ebiten.KeyAltLeft) || ebiten.IsKeyPressed(ebiten.KeyAltRight)
}

// OnUpdate registers a callback invoked once per frame, after the blink clock
// and fullscreen hotkeys are handled. Returning a non-nil error stops Run;
// return ebiten.Termination to quit cleanly. Passing nil clears the callback.
func (t *Text) OnUpdate(fn func() error) { t.update = fn }

// Update advances the blink clock, handles the fullscreen hotkeys, and runs the
// OnUpdate callback. It satisfies ebiten.Game.
func (t *Text) Update() error {
	t.ticks++
	if t.fsKeys {
		f11 := inpututil.IsKeyJustPressed(ebiten.KeyF11)
		altEnter := altPressed() && inpututil.IsKeyJustPressed(ebiten.KeyEnter)
		if f11 || altEnter {
			t.ToggleFullscreen()
		}
	}
	if t.update != nil {
		return t.update()
	}
	return nil
}

// Draw renders the screen. The native 720x400 image is scaled by the largest
// integer that fits the window and centred on a black background, keeping pixels
// crisp in both windowed and fullscreen modes. It satisfies ebiten.Game.
func (t *Text) Draw(screen *ebiten.Image) {
	nativeW := t.cols * CellWidth
	nativeH := t.rows * CellHeight
	if t.screen == nil {
		t.screen = ebiten.NewImage(nativeW, nativeH)
	}
	t.render()
	t.screen.WritePixels(t.fb)

	screen.Fill(color.RGBA{A: 0xff})

	b := screen.Bounds()
	fit := max(min(b.Dx()/nativeW, b.Dy()/nativeH), 1)
	ox := float64((b.Dx() - nativeW*fit) / 2)
	oy := float64((b.Dy() - nativeH*fit) / 2)

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(fit), float64(fit))
	op.GeoM.Translate(ox, oy)
	screen.DrawImage(t.screen, op)
}

// Snapshot renders the current screen to an image at native resolution
// (cols*CellWidth by rows*CellHeight). It is handy for headless tests and screen
// captures and does not require a running game.
func (t *Text) Snapshot() *image.RGBA {
	t.render()
	img := image.NewRGBA(image.Rect(0, 0, t.cols*CellWidth, t.rows*CellHeight))
	copy(img.Pix, t.fb)
	return img
}

// Layout maps the logical screen to device pixels (using the monitor's scale
// factor) so the offscreen image is placed and scaled in real pixels. It
// satisfies ebiten.Game.
func (t *Text) Layout(outsideWidth, outsideHeight int) (int, int) {
	dpr := ebiten.Monitor().DeviceScaleFactor()
	if dpr <= 0 {
		dpr = 1
	}
	if outsideWidth <= 0 || outsideHeight <= 0 {
		outsideWidth = t.cols * CellWidth * t.scale
		outsideHeight = t.rows * CellHeight * t.scale
	}
	w := int(math.Round(float64(outsideWidth) * dpr))
	h := int(math.Round(float64(outsideHeight) * dpr))
	return w, h
}

func (t *Text) attrBlinkOn() bool   { return (t.ticks/attrBlinkPeriod)%2 == 0 }
func (t *Text) cursorBlinkOn() bool { return (t.ticks/cursorBlinkPeriod)%2 == 0 }

// attrColors decodes an attribute byte into foreground and background colours.
// blinkPhaseOn is the current blink phase; in the off phase a blinking glyph is
// hidden by painting it in its background colour.
func (t *Text) attrColors(attr byte, blinkPhaseOn bool) (fg, bg color.RGBA) {
	fg = palette[attr&0x0f]
	if !t.blinkAttr {
		bg = palette[(attr>>4)&0x0f]
		return fg, bg
	}

	bg = palette[(attr>>4)&0x07]
	if attr&0x80 != 0 && !blinkPhaseOn {
		fg = bg
	}
	return fg, bg
}

func (t *Text) render() {
	w := t.cols * CellWidth
	blinkPhaseOn := t.attrBlinkOn()

	for cy := range t.rows {
		for cx := range t.cols {
			i := (cy*t.cols + cx) * 2
			code := t.mem[i]
			fg, bg := t.attrColors(t.mem[i+1], blinkPhaseOn)
			x0 := cx * CellWidth

			for row := range CellHeight {
				line := glyphRow(t.font, code, row)
				py := cy*CellHeight + row
				o := (py*w + x0) * 4
				mask := uint16(1) << (CellWidth - 1) // top bit = column 0
				for range CellWidth {
					c := bg
					if line&mask != 0 {
						c = fg
					}
					t.fb[o] = c.R
					t.fb[o+1] = c.G
					t.fb[o+2] = c.B
					t.fb[o+3] = 0xff
					o += 4
					mask >>= 1
				}
			}
		}
	}

	t.drawCursor(w)
}

func (t *Text) drawCursor(w int) {
	if !t.cur.visible || !t.cursorBlinkOn() || !t.inBounds(t.cur.col, t.cur.row) {
		return
	}

	fg := palette[t.mem[(t.cur.row*t.cols+t.cur.col)*2+1]&0x0f]
	x0 := t.cur.col * CellWidth
	for row := t.cur.start; row <= t.cur.end && row < CellHeight; row++ {
		if row < 0 {
			continue
		}
		py := t.cur.row*CellHeight + row
		o := (py*w + x0) * 4
		for range CellWidth {
			t.fb[o] = fg.R
			t.fb[o+1] = fg.G
			t.fb[o+2] = fg.B
			t.fb[o+3] = 0xff
			o += 4
		}
	}
}

// Ensure Text satisfies ebiten.Game at compile time.
var _ ebiten.Game = (*Text)(nil)
