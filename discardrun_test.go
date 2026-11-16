package linebreak

import "testing"

// ⛔⛔ A BREAK DISCARDS A RUN, NOT AN ITEM.
//
// tex.web:16506-16511: break_width is the background "minus the width of nodes
// following cur_p that are eliminated after a break … we also look ahead to
// eliminate all subsequent glue and penalty and kern and math nodes". The loop
// is tex.web:16517-16534 and it stops only at a char node (a box here) or a node
// of some other type.
//
// This package used to discard exactly one glue, so every line following a break
// with more than one discardable behind it was measured with width — and stretch,
// and shrink — that TeX had removed. These tests read Line.Start, because that is
// the one place the decision is observable from outside: a Start one item short of
// where the text really begins is the same defect as a width one glue too long.
func TestABreakDiscardsTheWholeRun(t *testing.T) {
	// \parfillskip as TeX writes it, so the last line's slack does not come from
	// whichever item the run happened to leave behind (see orders_test.go).
	end := []Item{
		Penalty(0, InfPenalty, false),
		GlueOrder(0, 1, OrderFil, 0),
		Penalty(0, -InfPenalty, false),
	}
	with := func(it ...Item) []Item { return append(append([]Item{}, it...), end...) }

	for _, c := range []struct {
		name  string
		items []Item
		// wantStart is the Start of the SECOND line: where the text after the
		// forced break really begins.
		wantStart int
		why       string
	}{
		{
			name: "a glue break followed by another glue",
			// 0:Box 1:Glue 2:Box 3:Penalty(forced) 4:Glue 5:Glue 6:Box
			// Position 5 is not a legal break (glue not after a box), so the run
			// has to carry past it on its own.
			items: with(Box(10), Glue(1, 1, 0.5), Box(10),
				Penalty(0, -InfPenalty, false), Glue(1, 1, 0.5), Glue(1, 1, 0.5), Box(10)),
			wantStart: 6,
			why:       "the second glue stayed on the line, which was then 1pt too long",
		},
		{
			name: "a PENALTY break followed by glue",
			// 0:Box 1:Glue 2:Box 3:Penalty(forced) 4:Glue 5:Box
			// ⭐ The common case, and the one a one-item rule got wrong in BOTH
			// directions: the break is not a glue, so nothing was discarded at all.
			items: with(Box(10), Glue(1, 1, 0.5), Box(10),
				Penalty(0, -InfPenalty, false), Glue(1, 1, 0.5), Box(10)),
			wantStart: 5,
			why:       "a penalty is stepped over, so the glue behind it goes too",
		},
		{
			name: "the run STOPS at a box",
			// 0:Box 1:Glue 2:Box 3:Penalty(forced) 4:Glue 5:Box 6:Glue 7:Box
			// If the run ran on past the box it would swallow the glue at 6 and the
			// second line would lose a word's worth of space.
			items: with(Box(10), Glue(1, 1, 0.5), Box(10),
				Penalty(0, -InfPenalty, false), Glue(1, 1, 0.5), Box(10), Glue(1, 1, 0.5), Box(10)),
			wantStart: 5,
			why:       "the run must end at the first box, as tex.web's `goto done` does",
		},
		{
			// ⛔ THIS ROW WAS MISLABELLED, and the mutation run caught it. It was
			// called "a glue break followed by a box: exactly one item, as before"
			// and offered as the control that the ordinary case had not moved — but
			// the break here is the forced PENALTY at 3, not a glue, so it moved
			// too, and a control that moves with the change is not a control.
			// The real one is TestAGlueBreakBeforeABoxStillDiscardsExactlyOneItem
			// below, which both implementations pass.
			name: "a penalty break followed by a box",
			// 0:Box 1:Glue 2:Box 3:Penalty(forced) 4:Box
			items: with(Box(10), Glue(1, 1, 0.5), Box(10),
				Penalty(0, -InfPenalty, false), Box(10)),
			wantStart: 4,
			why:       "the penalty is stepped over and the box stops the run at once",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			// ⚠ The measure is 21, which is `Box(10) Glue(1) Box(10)` exactly, so the
			// first line fits at ratio 0 and the forced penalty is the only feasible
			// route. A wide measure made every line hopeless and the whole paragraph
			// infeasible — the fixtures said nothing about discarding then, they just
			// failed.
			lines, ok := KnuthPlassWith(c.items, 21, DefaultParams(200, 10))
			if !ok {
				t.Fatalf("aucune coupure recevable")
			}
			if len(lines) != 2 {
				t.Fatalf("%d ligne(s), want 2 (la pénalité forcée coupe): %+v", len(lines), lines)
			}
			if lines[1].Start != c.wantStart {
				t.Errorf("la 2e ligne commence à %d, want %d — %s (tex.web:16517-16534)",
					lines[1].Start, c.wantStart, c.why)
			}
		})
	}
}

// ⛔⛔ THE CONTROL, and it has to PASS under the old rule as well as the new one —
// that is what makes it a control rather than another case of the change.
//
// A break at a glue with a box right behind it discards exactly that glue: one
// item, which is what this package already did and what tex.web does. If this
// moved, the run would have broken the ordinary case — every justified line in
// every paragraph — to fix the rare one.
//
// ⚠ It asserts lineStart directly rather than through the breaker, and that is
// deliberate: the first attempt at this control went through KnuthPlassWith with a
// forced penalty, so the break it actually tested was the PENALTY and not the glue.
// It failed under the mutation along with the rows it was supposed to control, and
// that is how the mislabel was found.
func TestAGlueBreakBeforeABoxStillDiscardsExactlyOneItem(t *testing.T) {
	items := []Item{Box(10), Glue(1, 1, 0.5), Box(10), Glue(1, 1, 0.5), Box(10)}
	for _, a := range []int{1, 3} {
		if got := lineStart(items, a, len(items)); got != a+1 {
			t.Errorf("lineStart(%d) = %d, want %d — a glue with a box behind it is one "+
				"item discarded, exactly as before", a, got, a+1)
		}
	}
}

// ⛔ AND THE PARAGRAPH'S BEGINNING IS NOT A BREAK. position 0 seeds the active
// list; there is nothing in front of it to discard, and tex.web:17001 says so:
// `prev_p:=cur_p; {glue at beginning is not a legal breakpoint}`. Discarding there
// threw away \noindent\hskip 20pt's 20pt (go-tex/engine#657).
//
// initialglue_test.go already guards that through the breaker; this asserts it on
// lineStart itself, because the run loop is a new place it could be lost.
func TestTheParagraphsOpeningGlueIsNotDiscarded(t *testing.T) {
	items := []Item{Glue(20, 0, 0), Box(10), Glue(1, 1, 0.5), Box(10)}
	if got := lineStart(items, 0, len(items)); got != 0 {
		t.Errorf("lineStart(0) = %d, want 0 — the paragraph's own opening glue is "+
			"part of its first line, not something a break threw away", got)
	}
	// Negative is not reachable from the breaker, but a helper that is called with
	// one must not walk off.
	if got := lineStart(items, -1, len(items)); got != -1 {
		t.Errorf("lineStart(-1) = %d, want -1", got)
	}
}

// ⛔⛔ A LINE MADE OF NOTHING BUT DISCARDABLES MUST COME OUT EMPTY, NOT NEGATIVE.
// Two adjacent penalties are both legal breakpoints, so a span whose every item is
// discarded is reachable. Without the limit the run would walk past the break the
// line ends at and the width would be computed backwards — a negative length, which
// reads as a line with room to spare.
func TestTheRunStopsAtTheBreakTheLineEndsAt(t *testing.T) {
	// A span from 1 to 3 holding only a penalty and a glue.
	items := []Item{Box(10), Penalty(0, 0, false), Glue(1, 1, 0.5), Penalty(0, 0, false), Box(10)}
	if got := lineStart(items, 1, 3); got != 3 {
		t.Errorf("lineStart(1, limit 3) = %d, want 3 — the run must not pass the "+
			"break the line ends at", got)
	}
	// The control: with room to run, it goes to the box.
	if got := lineStart(items, 1, len(items)); got != 4 {
		t.Errorf("lineStart(1, limit %d) = %d, want 4", len(items), got)
	}
}

// ⚠ TWO OF tex.web's FOUR CASES ARE NOT REACHABLE FROM HERE, and this test is
// that statement written down rather than left to a comment. §16528-16529
// discards an EXPLICIT kern and §16527 a math node; there is no KKern and no
// KMath, so a consumer maps both to KBox (go-typeset/linebreak#12 item 8 reports
// the same gap for legal()), and a KBox ends the run.
//
// So this asserts the CURRENT contract — a box ends the run, whatever it stands
// for — and names what it costs. The day the kinds exist, this test is the one
// that has to change, which is how the gap stays visible.
func TestAKernMappedToABoxEndsTheRunAndThatIsTheKnownGap(t *testing.T) {
	// If the consumer had an explicit kern here, TeX would discard it; mapped to a
	// box it stops the run.
	items := []Item{Box(10), Penalty(0, 0, false), Glue(1, 1, 0.5), Box(2 /* an explicit kern */), Box(10)}
	if got := lineStart(items, 1, len(items)); got != 3 {
		t.Errorf("lineStart = %d, want 3: a KBox ends the run. If this now returns 4, "+
			"the kern kind has arrived and §16528's half of the rule is reachable — "+
			"update this test and the note on lineStart rather than the assertion", got)
	}
}
