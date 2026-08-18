package linebreak

import (
	"math"
	"testing"
)

func para(nwords int, wordW, glueW, st, sh float64) []Item {
	var it []Item
	for i := 0; i < nwords; i++ {
		it = append(it, Box(wordW))
		if i < nwords-1 {
			it = append(it, Glue(glueW, st, sh))
		}
	}
	it = append(it, Glue(0, 1e6, 0))                // finishing glue fills the last line
	it = append(it, Penalty(0, -InfPenalty, false)) // forced break at end
	return it
}
func TestKnuthPlassEven(t *testing.T) {
	// 6 words w=2, glue 1/1/0.5, line=8 → "w g w g w" = 8 exactly → 2 lines of 3, r≈0
	items := para(6, 2, 1, 1, 0.5)
	lines, ok := KnuthPlass(items, 8, 5, 10)
	if !ok {
		t.Fatal("no feasible breaking")
	}
	if len(lines) != 2 {
		t.Fatalf("lines=%d want 2: %+v", len(lines), lines)
	}
	for i, l := range lines[:1] { // first line should be tight (r≈0)
		if math.Abs(l.Ratio) > 1e-6 {
			t.Errorf("line %d ratio=%.3f want 0", i, l.Ratio)
		}
	}
}
func TestKnuthPlassStretch(t *testing.T) {
	// 5 words w=2, glue 1/2/0.5, line=10. First line = 3 words (natural 8, +2 over stretch 4
	// → r=0.5); the last line carries \parfillskip. So the FIRST line must show r=0.5.
	items := para(5, 2, 1, 2, 0.5)
	lines, ok := KnuthPlass(items, 10, 5, 10)
	if !ok || len(lines) < 2 {
		t.Fatalf("ok=%v lines=%d want >=2", ok, len(lines))
	}
	if math.Abs(lines[0].Ratio-0.5) > 1e-6 {
		t.Errorf("first-line ratio=%.3f want 0.5", lines[0].Ratio)
	}
}

func TestKnuthPlassShrink(t *testing.T) {
	// 4 words w=3, glue 1/0.5/1, line=6. First line = 2 words: natural 3+1+3=7, over by 1,
	// shrink available 1 → r=-1 (fully shrunk, still feasible).
	items := para(4, 3, 1, 0.5, 1)
	lines, ok := KnuthPlass(items, 6, 5, 10)
	if !ok || len(lines) < 2 {
		t.Fatalf("ok=%v lines=%d", ok, len(lines))
	}
	if math.Abs(lines[0].Ratio+1) > 1e-6 {
		t.Errorf("first-line ratio=%.3f want -1", lines[0].Ratio)
	}
}
func TestKnuthPlassInfeasible(t *testing.T) {
	// A single unbreakable box far wider than the line, tolerance 5 → no feasible breaking
	// except the forced end (which is then overfull); KnuthPlass still returns the forced line.
	items := []Item{Box(100), Glue(0, 1e6, 0), Penalty(0, -InfPenalty, false)}
	lines, ok := KnuthPlass(items, 10, 5, 10)
	if !ok || len(lines) != 1 {
		t.Fatalf("forced-end: ok=%v lines=%d", ok, len(lines))
	}
}
