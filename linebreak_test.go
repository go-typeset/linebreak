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
	return withTerminator(it)
}

// withTerminator closes a paragraph the way TeX does, tex.web §16077-16084:
//
//	if is_char_node(tail) then tail_append(new_penalty(inf_penalty))
//	else if type(tail)<>glue_node then tail_append(new_penalty(inf_penalty))
//	else begin type(tail):=penalty_node; … penalty(tail):=inf_penalty; end;
//	link(tail):=new_param_glue(par_fill_skip_code);
//
// ⛔⛔ THE INFINITE PENALTY IN FRONT OF \parfillskip IS NOT DECORATION, AND THESE
// FIXTURES WERE BUILT WITHOUT IT. §16066-16071 says why in its own words: the
// \parfillskip "is preceded by an infinite penalty, so it will never be considered as
// a potential breakpoint". Omitted, the finishing glue IS a legal breakpoint — a glue
// after a box — so every fixture here offered the breaker one breakpoint TeX never
// offers, right at the end of the paragraph.
//
// It mattered: TestKnuthPlassInfeasible's single unbreakable box had its only "legal"
// break at that glue. The paragraph TeX builds there has exactly ONE breakpoint, the
// forced end, and yields one overfull line; the fixture's had two.
//
// The trailing forced penalty stays: TeX's line_break ends the last line by running
// out of list, and this breaker wants the end named.
func withTerminator(it []Item) []Item {
	if n := len(it); n > 0 && it[n-1].Kind == KGlue {
		it[n-1] = Penalty(0, InfPenalty, false) // a trailing glue is REPLACED
	} else {
		it = append(it, Penalty(0, InfPenalty, false))
	}
	it = append(it, Glue(0, 1e6, 0))                // \parfillskip
	it = append(it, Penalty(0, -InfPenalty, false)) // end of paragraph
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
	// The tolerance is a badness (LaTeX's \tolerance=200): r=0.5 is badness 12.5.
	items := para(5, 2, 1, 2, 0.5)
	lines, ok := KnuthPlass(items, 10, 200, 10)
	if !ok || len(lines) < 2 {
		t.Fatalf("ok=%v lines=%d want >=2", ok, len(lines))
	}
	if math.Abs(lines[0].Ratio-0.5) > 1e-6 {
		t.Errorf("first-line ratio=%.3f want 0.5", lines[0].Ratio)
	}
}

func TestKnuthPlassShrink(t *testing.T) {
	// 4 words w=3, glue 1/0.5/1, line=6. First line = 2 words: natural 3+1+3=7, over by 1,
	// shrink available 1 → r=-1 (fully shrunk, still feasible: badness 100, under
	// LaTeX's \tolerance=200).
	items := para(4, 3, 1, 0.5, 1)
	lines, ok := KnuthPlass(items, 6, 200, 10)
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
	// ⛔ Through withTerminator, so the \parfillskip carries the infinite penalty TeX
	// puts in front of it. Written by hand without it, the finishing glue was a legal
	// breakpoint and this paragraph had TWO — where TeX's has exactly one, the forced
	// end, and sets the box as a single overfull line.
	items := withTerminator([]Item{Box(100)})
	lines, ok := KnuthPlass(items, 10, 5, 10)
	if !ok || len(lines) != 1 {
		t.Fatalf("forced-end: ok=%v lines=%d", ok, len(lines))
	}
}
