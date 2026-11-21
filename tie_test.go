package linebreak

import "testing"

// tex.web §855 (:16848): `if d<=minimal_demerits[fit_class]`. Two breaks that tie
// on demerits are resolved in favour of the LATER one.
//
// The fixture: a fil in the first line and in \parfillskip makes every line
// badness 0, so the two ways of setting two lines — break at A (index 3) or at B
// (index 5) — cost exactly the same. TeX breaks at B.
func TestATieOnDemeritsKeepsTheLaterBreak(t *testing.T) {
	items := []Item{
		Box(0),                        // 0
		GlueOrder(0, 1, OrderFil, 0),  // 1 fil: every line stretching it is perfect
		Box(2),                        // 2
		Glue(1, 0, 0),                 // 3 break A
		Box(2),                        // 4
		Glue(1, 0, 0),                 // 5 break B
		Box(2),                        // 6
		Penalty(0, InfPenalty, false), // 7
		GlueOrder(0, 1, OrderFil, 0),  // 8 \parfillskip
	}
	lines, ok := KnuthPlassWith(items, 6, DefaultParams(200, 10))
	if !ok || len(lines) != 2 {
		t.Fatalf("ok=%v, %d lines, want 2", ok, len(lines))
	}
	if lines[0].End != 5 {
		t.Errorf("first line ends at %d, want 5: a tie must keep the later break", lines[0].End)
	}
}
