// Package pairqr turns a pairing link into a QR code module matrix, and
// draws a matrix in a terminal. The panel draws the same matrix as SVG.
package pairqr

import (
	"strings"

	"rsc.io/qr"
)

// quiet is the margin of light modules Terminal adds around the code.
const quiet = 2

// Matrix encodes text at error correction level M and returns the modules
// row by row, '1' for dark and '0' for light, with no quiet zone.
func Matrix(text string) ([]string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	rows := make([]string, c.Size)
	var b strings.Builder
	for y := range c.Size {
		b.Reset()
		for x := range c.Size {
			if c.Black(x, y) {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		rows[y] = b.String()
	}
	return rows, nil
}

// Terminal draws rows with half blocks, two module rows per line, inside a
// quiet zone of two modules.
func Terminal(rows []string) string {
	width := quiet*2 + len(rows)
	if len(rows) > 0 {
		width = quiet*2 + len(rows[0])
	}
	dark := func(y, x int) bool {
		y, x = y-quiet, x-quiet
		return y >= 0 && y < len(rows) && x >= 0 && x < len(rows[y]) && rows[y][x] == '1'
	}
	var b strings.Builder
	for y := 0; y < len(rows)+quiet*2; y += 2 {
		for x := range width {
			switch top, bottom := dark(y, x), dark(y+1, x); {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
