// Package vgarender simulates a 1990s VGA terminal, starting with the 80x25
// text mode (9x16 cells, 720x400), rendered with Ebitengine.
//
// The character/attribute buffer mirrors real VGA text memory at 0xB8000: two
// bytes per cell, a code point byte followed by an attribute byte. Rendering
// reproduces the authentic VGA "9-dot" column for the line-drawing glyphs
// 0xC0..0xDF and uses the standard 16-colour palette.
package vgarender
