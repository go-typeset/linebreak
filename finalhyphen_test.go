package linebreak

import "testing"

// ⛔⛔ \finalhyphendemerits WAS UNREACHABLE, AND A SECOND DEFECT HID THE FIRST.
//
// tex.web:16906-16908 charges one of two parameters when a break and the break
// before it are both hyphens:
//
//	if (break_type=hyphenated)and(type(r)=hyphenated) then
//	  if cur_p<>null then d:=d+double_hyphen_demerits
//	  else d:=d+final_hyphen_demerits;
//
// and `cur_p=null` is not the end of the item list — it is the DISTINGUISHED
// final call, tex.web:17197-17198:
//
//	@<Try the final line break at the end of the paragraph...@>=
//	begin try_break(eject_penalty,hyphenated);
//
// `break_type` is `hyphenated` there whatever item sits at the end. The caller
// passed `atEnd = b == n` (the paragraph's last line ends at `paraEnd`, the
// forced penalty) AND `flagged = flaggedAt(items, b)` (at that break a
// `\penalty-10000`, never flagged). So \finalhyphendemerits could not be
// reached, \doublehyphendemerits stood in for it, and because the second
// condition was false too, NEITHER was charged: a hyphen on the last line but
// one was free. go-typeset/linebreak#12 item (3) named the first half.
//
// This test is behavioural on purpose. `TestFinalHyphenCostsLessThanADoubleOne`
// calls `demerits` directly with `atEnd` true and false by hand, so it proves
// the FUNCTION distinguishes them while nothing ever reached the branch — a unit
// test of unreachable code, green for two releases.
//
// The paragraph below has exactly two three-line solutions that differ only in
// whether the penultimate break is a hyphen:
//
//	A  break at the hyphen (item 7): line 2 is exactly the measure, badness 0
//	B  break at the space  (item 9): line 2 is fully shrunk, badness 100
//
// So A is far cheaper on badness — (10+0)² against (10+100)², a difference of
// 12000 — and wins whenever the hyphen is free. Charging \finalhyphendemerits
// makes A dearer than B and the breaker must take the space instead.
//
// ⛔ AND \doublehyphendemerits IS SET TO ZERO, which is the control that makes
// this test tell the two parameters apart: an implementation charging the
// double-hyphen parameter here — the exact shape of the old defect — charges
// nothing, and A wins again.
func TestAHyphenOnTheLastLineButOneIsCharged(t *testing.T) {
	const measure = 10
	items := []Item{
		Box(4),                         // 0
		Glue(1, 4, 1),                  // 1  line 1 may end here
		Box(4),                         // 2
		Glue(1, 4, 1),                  // 3  line 1 ends here in both solutions
		Box(4),                         // 4
		Glue(1, 4, 2),                  // 5  the space inside line 2
		Box(5),                         // 6
		Penalty(0, 50, true),           // 7  A: the hyphen — line 2 = 4+1+5 = 10 exactly
		Box(2),                         // 8
		Glue(1, 4, 1),                  // 9  B: the space — line 2 = 4+1+5+2 = 12, shrunk to 10
		Box(4),                         // 10
		GlueOrder(0, 1, OrderFil, 0),   // 11 \parfillskip
		Penalty(0, -InfPenalty, false), // 12 the paragraph's forced end
	}

	breaks := func(p Params) []int {
		t.Helper()
		lines, ok := KnuthPlassWith(items, measure, p)
		if !ok {
			t.Fatalf("no solution")
		}
		out := make([]int, 0, len(lines))
		for _, l := range lines {
			out = append(out, l.End)
		}
		return out
	}

	p := DefaultParams(200, 10)
	p.DoubleHyph = 0 // the control: only \finalhyphendemerits can decide this
	p.FinalHyph = 0
	free := breaks(p)
	if len(free) != 3 || free[1] != 7 {
		t.Fatalf("with the hyphen free the breaker should take it: lines end at %v, "+
			"want three lines with the second ending at item 7 (the hyphen)", free)
	}

	p.FinalHyph = 50000
	charged := breaks(p)
	if len(charged) != 3 {
		t.Fatalf("%d lines, want 3: the charge should change WHICH break is taken, "+
			"not how many lines there are — got %v", len(charged), charged)
	}
	if charged[1] == 7 {
		t.Errorf("the penultimate break is still the hyphen (item 7) with "+
			"\\finalhyphendemerits = 50000: it is not being charged. lines end at %v", charged)
	}
	if charged[1] != 9 {
		t.Errorf("the penultimate break is item %d, want 9 (the space): %v", charged[1], charged)
	}
}

// And the other half of the rule, so the fix cannot be "charge it everywhere":
// a hyphen in the MIDDLE of a paragraph still costs \doublehyphendemerits, and
// it is the final break alone that costs \finalhyphendemerits. Here
// \finalhyphendemerits is zero and \doublehyphendemerits is the large one, so
// only a mid-paragraph charge can move the break.
func TestAMidParagraphDoubleHyphenIsStillTheDoubleOne(t *testing.T) {
	const measure = 10
	items := []Item{
		Box(4),                         // 0
		Glue(1, 4, 1),                  // 1
		Box(4),                         // 2
		Penalty(0, 50, true),           // 3  a hyphen ending line 1
		Box(4),                         // 4
		Glue(1, 4, 2),                  // 5
		Box(5),                         // 6
		Penalty(0, 50, true),           // 7  a second hyphen, line 2 exactly the measure
		Box(2),                         // 8
		Glue(1, 4, 1),                  // 9
		Box(4),                         // 10
		Glue(1, 4, 1),                  // 11
		Box(4),                         // 12  one more line, so item 7 is NOT the last but one
		GlueOrder(0, 1, OrderFil, 0),   // 13
		Penalty(0, -InfPenalty, false), // 14
	}
	p := DefaultParams(200, 10)
	p.FinalHyph = 0
	p.DoubleHyph = 0
	lines, ok := KnuthPlassWith(items, measure, p)
	if !ok {
		t.Fatal("no solution")
	}
	var free []int
	for _, l := range lines {
		free = append(free, l.End)
	}
	p.DoubleHyph = 50000
	lines, ok = KnuthPlassWith(items, measure, p)
	if !ok {
		t.Fatal("no solution with the charge")
	}
	var charged []int
	for _, l := range lines {
		charged = append(charged, l.End)
	}
	if len(free) < 3 || free[1] != 7 {
		t.Fatalf("with both charges zero the second line should end at the hyphen 7: %v", free)
	}
	if len(charged) >= 2 && charged[1] == 7 {
		t.Errorf("\\doublehyphendemerits = 50000 did not move the second of two "+
			"consecutive hyphen breaks: %v (free: %v)", charged, free)
	}
}
