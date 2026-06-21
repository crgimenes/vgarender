// Command defrag is an animated homage to the 1990s DOS disk optimizers, drawn
// in the VGA 80x25 text mode. A write frontier sweeps from the left, leaving a
// contiguous "optimized" region behind it, while a read head jumps around the
// fragmented area ahead pulling clusters to relocate.
//
// The relocation idea is borrowed from the PHPUnit "defrag" extension by Ben
// Holmen (MIT); the look follows period screenshots. No trademarked names used.
//
// F11 or Alt+Enter toggles fullscreen; Esc quits.
package main

import (
	"fmt"
	"log"
	"strconv"

	"github.com/crgimenes/vgarender"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// CP437 glyphs used by the map.
const (
	chDot   = 0xfa // · cluster
	chShade = 0xb0 // ░ free space
)

// Layout of the 80x25 screen.
const (
	scrCols, scrRows = 80, 25
	mapX0, mapY0     = 1, 2 // top-left cell inside the map frame
	mapW, mapH       = 78, 14
	nSect            = mapW * mapH // 1092 blocks
	clustersPerBlock = 54

	// Each cluster move waits stepBaseFrames give or take stepJitterFrames, so
	// the pace wobbles by a few hundredths of a second instead of being rigid
	// (at 60 fps, one frame is ~0.017 s).
	stepBaseFrames   = 6
	stepJitterFrames = 3
)

// Palette indices (blink is disabled, so backgrounds 8..15 are bright).
const (
	cBlue   = 9  // desktop / disk background
	cYellow = 14 // optimized region
	cBlack  = 0
	cWhite  = 15
	cDkBlue = 1  // read/write head background
	cGray   = 8  // progress-bar remainder
	cRed    = 12 // help line
)

// sector is a render state for one map cell.
type sector uint8

const (
	free sector = iota
	used
	done
	reading
	writing
	unmovable
	bad
)

// attr builds a VGA attribute byte from foreground and background indices.
func attr(fg, bg byte) byte { return (bg&0x0f)<<4 | fg&0x0f }

// cell returns the glyph and attribute that draw a sector state.
func cell(s sector) (ch, a byte) {
	switch s {
	case free:
		return chShade, attr(cWhite, cBlue)
	case used:
		return chDot, attr(cWhite, cBlue)
	case done:
		return chDot, attr(cBlue, cYellow)
	case reading:
		return 'r', attr(cWhite, cDkBlue)
	case writing:
		return 'W', attr(cWhite, cDkBlue)
	case unmovable:
		return 'X', attr(cWhite, cBlue)
	case bad:
		return 'B', attr(cRed, cBlue)
	}
	return ' ', attr(cWhite, cBlue)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

func center(s string, w int) string {
	if len(s) >= w {
		return s[:w]
	}
	left := (w - len(s)) / 2
	return spaces(left) + s + spaces(w-len(s)-left)
}

// leftRight places left and right at the edges of a w-wide field.
func leftRight(left, right string, w int) string {
	if len(left)+len(right) >= w {
		return (left + right)[:w]
	}
	return left + spaces(w-len(left)-len(right)) + right
}

func commafy(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	pre := len(s) % 3
	var out []byte
	if pre > 0 {
		out = append(out, s[:pre]...)
		out = append(out, ',')
	}
	for i := pre; i < len(s); i += 3 {
		out = append(out, s[i:i+3]...)
		if i+3 < len(s) {
			out = append(out, ',')
		}
	}
	return string(out)
}

// rng is a small deterministic LCG (no crypto/PRNG dependency).
type rng struct{ s uint32 }

func (r *rng) next() uint32   { r.s = r.s*1664525 + 1013904223; return r.s }
func (r *rng) intn(n int) int { return int(r.next()>>1) % n }
func (r *rng) float() float64 { return float64(r.next()) / 4294967296.0 }

func boxLine(t *vgarender.Text, x, y, w, h int, a byte) {
	const (
		tl, tr, bl, br = 0xda, 0xbf, 0xc0, 0xd9 // ┌ ┐ └ ┘
		hz, vt         = 0xc4, 0xb3             // ─ │
	)
	t.SetCell(x, y, tl, a)
	t.SetCell(x+w-1, y, tr, a)
	t.SetCell(x, y+h-1, bl, a)
	t.SetCell(x+w-1, y+h-1, br, a)
	for i := 1; i < w-1; i++ {
		t.SetCell(x+i, y, hz, a)
		t.SetCell(x+i, y+h-1, hz, a)
	}
	for i := 1; i < h-1; i++ {
		t.SetCell(x, y+i, vt, a)
		t.SetCell(x+w-1, y+i, vt, a)
	}
}

func panel(t *vgarender.Text, x, y, w, h int, title string) {
	boxLine(t, x, y, w, h, attr(cWhite, cBlue))
	label := " " + title + " "
	t.Print(x+(w-len(label))/2, y, label, attr(cYellow, cBlue))
}

func legendEntry(t *vgarender.Text, x, y int, s sector, label string) {
	ch, a := cell(s)
	t.SetCell(x, y, ch, a)
	t.Print(x+1, y, " - "+label, attr(cWhite, cBlue))
}

type defrag struct {
	t                 *vgarender.Text
	disk              []sector // mutable working layout
	rng               rng
	totalUsed         int // used clusters to place (for the progress bar)
	doneCount         int // clusters already optimized
	head              int // write frontier; the next cell to process
	writePos          int // cell just written this step (W marker), -1 if none
	readPos           int // source just read this step (r marker), -1 if none
	stepTick          int // frames since the last cluster move
	nextStep          int // randomized frame count to wait before the next move
	finished          bool
	frame, startFrame int
	doneFrame         int
}

func newDefrag(t *vgarender.Text) *defrag {
	g := &defrag{t: t, disk: make([]sector, nSect), rng: rng{s: 1}}
	g.drawChrome()
	g.reset()
	return g
}

// reset lays out a fresh fragmented disk: mostly used toward the front, mostly
// free toward the back, with holes throughout and a few obstacles.
func (g *defrag) reset() {
	for i := range g.disk {
		pUsed := 0.92 - 0.85*float64(i)/float64(nSect)
		if g.rng.float() < pUsed {
			g.disk[i] = used
		} else {
			g.disk[i] = free
		}
	}
	for k := 0; k < 6; k++ {
		g.disk[g.rng.intn(nSect)] = unmovable
	}
	g.disk[g.rng.intn(nSect)] = bad

	g.totalUsed = 0
	for _, s := range g.disk {
		if s == used {
			g.totalUsed++
		}
	}

	g.head = 0
	g.doneCount = 0
	g.writePos, g.readPos = -1, -1
	g.stepTick = 0
	g.scheduleNext()
	g.finished = false
	g.doneFrame = 0
	g.startFrame = g.frame
}

// scheduleNext picks a randomized wait, in frames, before the next cluster move.
func (g *defrag) scheduleNext() {
	g.nextStep = stepBaseFrames + g.rng.intn(2*stepJitterFrames+1) - stepJitterFrames
	if g.nextStep < 1 {
		g.nextStep = 1
	}
}

// lastUsed returns the back-most still-fragmented cluster ahead of head, or -1.
func (g *defrag) lastUsed() int {
	for i := nSect - 1; i > g.head; i-- {
		if g.disk[i] == used {
			return i
		}
	}
	return -1
}

// step optimizes one cluster at the frontier. A used cluster already at the
// front simply becomes optimized; a hole is filled by relocating the back-most
// fragmented cluster, whose source is then freed.
func (g *defrag) step() {
	for g.head < nSect && (g.disk[g.head] == unmovable || g.disk[g.head] == bad) {
		g.head++ // obstacles stay put
	}
	if g.head >= nSect {
		g.finish()
		return
	}

	switch g.disk[g.head] {
	case used:
		g.disk[g.head] = done
		g.writePos, g.readPos = g.head, -1
	case free:
		src := g.lastUsed()
		if src < 0 {
			g.finish() // no fragmented clusters remain; the rest is free space
			return
		}
		g.disk[g.head] = done
		g.disk[src] = free // the relocated cluster's source becomes unused
		g.writePos, g.readPos = g.head, src
	default:
		g.head++
		return
	}
	g.doneCount++
	g.head++
}

func (g *defrag) finish() {
	g.finished = true
	g.writePos, g.readPos = -1, -1
}

// stateAt derives the current render state of cell i.
func (g *defrag) stateAt(i int) sector {
	switch {
	case i == g.writePos:
		return writing
	case i == g.readPos:
		return reading
	default:
		return g.disk[i]
	}
}

func (g *defrag) drawChrome() {
	t := g.t
	t.ShowCursor(false)
	t.SetBlinkEnabled(false) // allow bright backgrounds (yellow, light blue)
	t.Clear(attr(cWhite, cBlue))

	// Menu bar.
	for x := range scrCols {
		t.SetCell(x, 0, ' ', attr(cWhite, cBlue))
	}
	t.Print(0, 0, " Optimize ", attr(cWhite, cBlack))
	t.Print(scrCols-9, 0, "F1=Help", attr(cWhite, cBlue))

	// Disk map frame.
	boxLine(t, 0, 1, scrCols, 16, attr(cWhite, cBlue))

	// Status and legend panels.
	panel(t, 0, 18, 40, 6, "Status")
	panel(t, 41, 18, 39, 6, "Legend")
	t.Print(1, 22, center("Full Optimization", 38), attr(cWhite, cBlue))

	legendEntry(t, 42, 19, used, "Used")
	legendEntry(t, 61, 19, free, "Unused")
	legendEntry(t, 42, 20, reading, "Reading")
	legendEntry(t, 61, 20, writing, "Writing")
	legendEntry(t, 42, 21, bad, "Bad")
	legendEntry(t, 61, 21, unmovable, "Unmovable")
	t.Print(42, 22, "Drive C:  1 block = 54 clusters", attr(cWhite, cBlue))

	// Bottom help line, right side (static).
	t.SetCell(scrCols-9, scrRows-1, 0xb3, attr(cRed, cBlue))
	t.Print(scrCols-7, scrRows-1, "Defrag", attr(cRed, cBlue))
}

func (g *defrag) drawDisk() {
	for i := range nSect {
		ch, a := cell(g.stateAt(i))
		g.t.SetCell(mapX0+i%mapW, mapY0+i/mapW, ch, a)
	}
}

func (g *defrag) drawStatus() {
	t := g.t

	pct := 0
	if g.totalUsed > 0 {
		pct = min(100, g.doneCount*100/g.totalUsed)
	}
	cluster := "Cluster " + commafy(g.doneCount*clustersPerBlock)
	t.Print(1, 19, leftRight(cluster, fmt.Sprintf("%d%%", pct), 38), attr(cWhite, cBlue))

	filled := pct * 38 / 100
	for k := range 38 {
		bg := byte(cGray)
		if k < filled {
			bg = cWhite
		}
		t.SetCell(1+k, 20, ' ', attr(cBlack, bg))
	}

	sec := (g.frame - g.startFrame) / 60
	elapsed := fmt.Sprintf("Elapsed Time: %02d:%02d:%02d", sec/3600, sec/60%60, sec%60)
	t.Print(1, 21, center(elapsed, 38), attr(cWhite, cBlue))

	action := "Reading and writing..."
	switch {
	case g.finished:
		action = "Finished."
	case (g.frame/45)%2 == 1:
		action = "Sorting..."
	}
	t.Print(1, scrRows-1, leftRight(action, "", 68), attr(cRed, cBlue))
}

func (g *defrag) update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return ebiten.Termination
	}

	g.frame++
	if !g.finished {
		g.stepTick++
		if g.stepTick >= g.nextStep {
			g.stepTick = 0
			g.step()
			g.scheduleNext()
		}
	}

	g.drawDisk()
	g.drawStatus()

	if g.finished {
		if g.doneFrame == 0 {
			g.doneFrame = g.frame
		}
		if g.frame-g.doneFrame > 180 { // hold the finished disk ~3s, then loop
			g.reset()
		}
	}
	return nil
}

func main() {
	t := vgarender.NewText(scrCols, scrRows, vgarender.WithScale(2))
	g := newDefrag(t)
	t.OnUpdate(g.update)

	if err := t.Run("Defragmenter"); err != nil {
		log.Fatal(err)
	}
}
