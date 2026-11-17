package linebreak

import "testing"

// loosePara is a paragraph with more than one decent ending.
//
// ⛔ THE GLUE HAS TO STRETCH FOR THERE TO BE A CHOICE AT ALL. With stretch 3 on a
// 50pt measure a three-word line needs ratio 2 — badness 800 — and the four-line
// breaking costs 676575 against the three-line's 563; the trace says so, and the
// pruning drops it long before \looseness is consulted. That is TeX's behaviour
// too, and it is why \looseness so often cannot loosen. With stretch 8 both
// breakings are decent and there is something to choose between.
func loosePara() []Item { return para(12, 10, 4, 8, 2) }

// looseParams carries \adjdemerits, which is NOT decoration here: the pruning at
// the end of each break position keeps only nodes within lowest+|AdjDemerits|
// (tex.web §16476-16487). Left at zero — this test's first draft — the cheapest
// ending is the only survivor and \looseness has an empty list to choose from,
// which reads exactly like "the parameter does not work".
func looseParams() Params {
	return Params{Tolerance: 5000, LinePenalty: 10, AdjDemerits: 10000}
}

// ⛔⛔ \looseness CHOOSES AMONG THE ENDINGS THAT ALREADY EXIST.
//
// tex.web:17197-17237. When the last break has been tried, every feasible way of
// ending the paragraph is an active node. TeX takes the fewest-demerits one,
// calls its line count best_line, and — only when \looseness is non-zero — walks
// the SAME nodes again for one whose line count differs from best_line in the
// direction asked for:
//
//	line_diff:=line_number(r)-best_line;
//	if ((line_diff<actual_looseness)and(looseness<=line_diff))or
//	   ((line_diff>actual_looseness)and(looseness>=line_diff)) then …
//
// Two things had to be true for that to work and neither was. The package kept
// only the cheapest ending, and it keyed its state by (position, fitness) where
// tex.web:16107-16114 keys by position, line AND fitness — collapsing the line
// dimension being an optimisation licensed "when the looseness parameter is
// zero" (:16716, `if looseness=0 then easy_line:=last_special_line`). So the
// parameter was stored and unread: go-typeset/linebreak#12 item (7).
func TestLoosenessPicksALongerBreaking(t *testing.T) {
	items, p := loosePara(), looseParams()

	base, ok := KnuthPlassShaped(items, Uniform(50), p)
	if !ok {
		t.Fatal("the fixture does not break at all")
	}
	if len(base) != 3 {
		t.Fatalf("the fixture sets in %d lines, want 3 — the rest of this test is "+
			"about which OTHER line count is available", len(base))
	}

	q := p
	q.Looseness = +1
	q.FinalPass = true
	got, ok := KnuthPlassShaped(items, Uniform(50), q)
	if !ok {
		t.Fatal("looseness +1: no breaking")
	}
	if len(got) != len(base)+1 {
		t.Errorf("looseness +1 gave %d lines, want %d: the longer ending is "+
			"feasible here and tex.web:17221-17237 must choose it",
			len(got), len(base)+1)
	}
}

// ⛔ A LOOSENESS THAT NOTHING CAN SATISFY IS NOT A FAILURE OF THE CHOICE.
//
// This fixture has no shorter ending: six words to the line is 80pt of material
// on a 50pt measure and shrinks to 70, so a two-line breaking is infeasible and
// \looseness=-1 cannot be honoured however the nodes are keyed. tex.web:17203 is
// explicit about what happens then —
//
//	if (actual_looseness=looseness)or final_pass then goto done
//
// — the final pass keeps what it reached, and any earlier pass FAILS so the
// caller runs a looser one. Both halves are asserted, because a change that
// simply returned the base breakings would pass the first.
func TestALoosenessThatCannotBeMetFailsOffTheFinalPass(t *testing.T) {
	items, p := loosePara(), looseParams()
	base, _ := KnuthPlassShaped(items, Uniform(50), p)

	q := p
	q.Looseness = -1
	q.FinalPass = true
	got, ok := KnuthPlassShaped(items, Uniform(50), q)
	if !ok || len(got) != len(base) {
		t.Errorf("looseness -1 on the final pass: %d line(s), ok=%v, want the %d "+
			"it reached", len(got), ok, len(base))
	}

	q.FinalPass = false
	if _, ok := KnuthPlassShaped(items, Uniform(50), q); ok {
		t.Error("looseness -1 off the final pass must FAIL, so the caller runs the " +
			"next and looser one (tex.web:17203)")
	}
}

// ⛔ TIES ON line_diff ARE SETTLED BY DEMERITS, which is the second arm of
// tex.web:17230-17233 and the one a whole-paragraph fixture reaches only by
// accident:
//
//	else if (line_diff=actual_looseness)and(total_demerits(r)<fewest_demerits) then
//	  begin best_bet:=r; fewest_demerits:=total_demerits(r); end
//
// Two endings of the SAME length are equally loose, so the rule has nothing to
// say about looseness and falls back to the ordinary criterion: take the cheaper.
// Tested on the function directly, because building a paragraph whose feasible
// endings tie on length and differ on cost would witness that arithmetic rather
// than this rule.
func TestChooseLoosenessSettlesTiesOnDemerits(t *testing.T) {
	// best has 3 lines; two candidates have 4 (line_diff +1) at different cost.
	best := &breakNode{position: 9, line: 3, demerits: 100}
	dear := &breakNode{position: 8, line: 4, demerits: 900}
	cheap := &breakNode{position: 7, line: 4, demerits: 300}

	got, matched := chooseLooseness([]*breakNode{best, dear, cheap}, best, +1)
	if !matched {
		t.Error("looseness +1 with a 4-line ending present: actual_looseness should be +1")
	}
	if got != cheap {
		t.Errorf("chose the ending at position %d (demerits %.0f), want the one at "+
			"%d (demerits %.0f): an equal line_diff is settled by demerits",
			got.position, got.demerits, cheap.position, cheap.demerits)
	}

	// ⛔ AND THE ORDER MUST NOT DECIDE IT. The same three the other way round: the
	// first arm takes `cheap` on its way up from actual_looseness 0, and the
	// tie-break must then refuse to replace it with the dearer one.
	got, _ = chooseLooseness([]*breakNode{best, cheap, dear}, best, +1)
	if got != cheap {
		t.Errorf("with the candidates reversed the choice moved to position %d: the "+
			"rule must not depend on the order the endings were recorded in",
			got.position)
	}
}

// ⛔ THE CONTROL: \looseness=0 is the behaviour this package always had, and the
// line-keyed state must not change it. Without this row the change above could
// have moved every ordinary paragraph and nothing would have said so.
func TestLoosenessZeroChangesNothing(t *testing.T) {
	items, p := loosePara(), looseParams()

	a, ok1 := KnuthPlassShaped(items, Uniform(50), p)
	q := p
	q.Looseness = 0
	b, ok2 := KnuthPlassShaped(items, Uniform(50), q)
	if !ok1 || !ok2 || len(a) != len(b) {
		t.Fatalf("\\looseness=0 changed the breaking: %d lines against %d", len(b), len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("line %d differs: %+v against %+v", i, b[i], a[i])
		}
	}

	// One box that cannot be broken anywhere: exactly one ending exists, and the
	// final pass takes it rather than refusing the paragraph.
	one := withTerminator([]Item{Box(200)})
	r := p
	r.Looseness = +1
	r.FinalPass = true
	if lines, ok := KnuthPlassShaped(one, Uniform(50), r); !ok || len(lines) != 1 {
		t.Errorf("an unloosenable paragraph came back as %d line(s), ok=%v", len(lines), ok)
	}
}
