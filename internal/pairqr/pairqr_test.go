package pairqr

import (
	"strings"
	"testing"
)

func TestMatrixIsSquareAndBinary(t *testing.T) {
	rows, err := Matrix("orca://pair?code=abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 21 {
		t.Fatalf("size = %d", len(rows))
	}
	for i, r := range rows {
		if len(r) != len(rows) || strings.Trim(r, "01") != "" {
			t.Fatalf("row %d = %q", i, r)
		}
	}
	if rows[0][:7] != "1111111" || rows[1][:7] != "1000001" {
		t.Errorf("no finder pattern: %q %q", rows[0][:7], rows[1][:7])
	}
}

func TestTerminal(t *testing.T) {
	out := Terminal([]string{"10", "01", "11"})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	// 3 rows + 2+2 quiet rows = 7 module rows → 4 lines; 2+2+2 = 6 columns.
	if len(lines) != 4 {
		t.Fatalf("lines = %q", lines)
	}
	want := []string{"      ", "  ▀▄  ", "  ▀▀  ", "      "}
	for i, l := range lines {
		if l != want[i] {
			t.Errorf("line %d = %q, want %q", i, l, want[i])
		}
	}
}
