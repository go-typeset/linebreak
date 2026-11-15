package linebreak

import (
	"math"
	"testing"
)

// ⛔⛔ A PENALTY'S WIDTH IS CHARGED TO THE LINE THAT BREAKS THERE AND TO NO
// OTHER. tex.web:17128-17137:
//
//	act_width:=act_width+disc_width;
//	try_break(hyphen_penalty,hyphenated);
//	act_width:=act_width-disc_width;
//
// It is added for one try_break and taken straight back, so a discretionary's
// pre-break material — the hyphen — belongs to the line that ends with it. The
// prefix sums here used to carry it, which charged it to every line that merely
// SPANNED the hyphenation point (go-typeset/linebreak#12 item 2).
//
// Both halves are asserted, because each alone passes while the other is broken:
// a reader of only the first could delete the add at the break, and a reader of
// only the second could put the width back in the sums.
//
// The fixtures avoid \parfillskip on purpose: an infinite stretch makes every
// ratio 0 and the widths would stop being observable.
func TestAPenaltysWidthIsChargedOnlyWhereItBreaks(t *testing.T) {
	// 4 + 6 = 10 exactly when the hyphen is NOT charged; 12 — overfull past a
	// shrink of zero — when it is.
	items := []Item{
		Box(4),                         // 0
		Penalty(2, 0, true),            // 1  a hyphen two units wide
		Box(6),                         // 2
		Glue(0, 0, 0),                  // 3
		Penalty(0, -InfPenalty, false), // 4  the forced end
	}

	t.Run("a line spanning it is not charged", func(t *testing.T) {
		lines, ok := KnuthPlassWith(items, 10, DefaultParams(200, 10))
		if !ok {
			t.Fatal("no solution: the span was measured as overfull, so the hyphen " +
				"is being charged to a line that does not break at it")
		}
		if len(lines) != 1 {
			t.Fatalf("%d lines, want 1 — the paragraph fits on one line of 10 unless "+
				"the hyphen is charged to it: %+v", len(lines), lines)
		}
		if math.Abs(lines[0].Ratio) > 1e-9 {
			t.Errorf("ratio = %v, want 0: the line is exactly the measure", lines[0].Ratio)
		}
	})

	t.Run("a line breaking at it is charged", func(t *testing.T) {
		// At a measure of 6, breaking at the hyphen gives 4+2 = 6 exactly. If the
		// width were not charged at the break the line would be 4 and need two
		// units of stretch it does not have.
		lines, ok := KnuthPlassWith(items, 6, DefaultParams(200, 10))
		if !ok {
			t.Fatal("no solution at a measure of 6")
		}
		if len(lines) != 2 {
			t.Fatalf("%d lines, want 2: %+v", len(lines), lines)
		}
		if lines[0].End != 1 {
			t.Fatalf("the first line ends at item %d, want 1 (the hyphen): %+v",
				lines[0].End, lines)
		}
		for i, l := range lines {
			if math.Abs(l.Ratio) > 1e-9 {
				t.Errorf("line %d ratio = %v, want 0 — line 1 is 4+2 and line 2 is 6, "+
					"both exactly the measure", i+1, l.Ratio)
			}
		}
	})
}

// And the width reaches the REPORTED line, not only the decision: measure() is
// called twice, once to choose the break and once to rebuild the lines, so a
// Ratio could describe a span its own Start and End do not.
//
// ⚠ THIS ONE DOES NOT DISCRIMINATE THE MUTATION ABOVE, and saying so is the
// point of the note. Putting the width back into the prefix sums double-charges
// a break-at line through BOTH call sites equally, so this test stays green
// while the test above goes red. It guards a different mutation — a fix applied
// to one call site and not the other — which the current structure makes
// unreachable, since both go through the same measure(). Keep it as the pin for
// that property, not as evidence for this fix.
func TestTheReportedLineCarriesTheHyphenItBreaksAt(t *testing.T) {
	// One line would be 3+1+3+2+1+2 = 12 against a measure of 9 with 2 of shrink,
	// so r = −1.5 and it is refused: the hyphen at item 3 is the only break that
	// leaves both lines feasible. Line 2 carries its own glue, or it could not
	// stretch to the measure and would be refused too.
	items := []Item{
		Box(3),                         // 0
		Glue(1, 1, 1),                  // 1
		Box(3),                         // 2
		Penalty(2, 0, true),            // 3  break here: 3+1+3+2 = 9 exactly
		Box(2),                         // 4
		Glue(1, 4, 1),                  // 5
		Box(2),                         // 6
		Glue(0, 0, 0),                  // 7
		Penalty(0, -InfPenalty, false), // 8
	}
	lines, ok := KnuthPlassWith(items, 9, DefaultParams(200, 10))
	if !ok {
		t.Fatal("no solution")
	}
	if len(lines) != 2 || lines[0].End != 3 {
		t.Fatalf("want two lines with the first ending at the hyphen (item 3): %+v", lines)
	}
	// 3+1+3+2 = 9 exactly, so the glue is neither stretched nor shrunk.
	if math.Abs(lines[0].Ratio) > 1e-9 {
		t.Errorf("the first line's ratio is %v, want 0: with the hyphen it is exactly "+
			"9, and without it 7 — which would read as stretched by 2", lines[0].Ratio)
	}
}
