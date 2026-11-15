// Copyright (c) the go-typeset authors.
// SPDX-License-Identifier: BSD-3-Clause

package linebreak

import (
	"math"
	"testing"
)

// The shape rules come from tex.web:16719-16732 and :16740-16748, and the two
// that are easy to get wrong are asserted by name below: the sign of the indent
// is not the sign of the margin, and a negative \hangafter counts the FIRST
// lines rather than skipping them.
func TestHangIndentFollowsTexWebsSignRules(t *testing.T) {
	const hsize = 100
	for _, tc := range []struct {
		name   string
		indent float64
		after  int
		want   []struct{ indent, width float64 } // lines 1,2,3
		why    string
	}{
		{
			name:   "positive indent after the first line",
			indent: 10, after: 1,
			want: []struct{ indent, width float64 }{{0, 100}, {10, 90}, {10, 90}},
			why:  "hang_after>=0: line 1 full, then hsize-|indent| with margin indent",
		},
		{
			name:   "⛔ NEGATIVE indent indents from the RIGHT: narrower, margin 0",
			indent: -10, after: 1,
			want: []struct{ indent, width float64 }{{0, 100}, {0, 90}, {0, 90}},
			why:  ":16729-16730 — the line is shortened by |indent| and the margin stays 0",
		},
		{
			name:   "⛔ NEGATIVE after means the FIRST |after| lines",
			indent: 10, after: -2,
			want: []struct{ indent, width float64 }{{10, 90}, {10, 90}, {0, 100}},
			why:  ":16720-16726 — lines 1..2 are the hung ones, line 3 is full",
		},
		{
			name:   "negative after AND negative indent",
			indent: -10, after: -1,
			want: []struct{ indent, width float64 }{{0, 90}, {0, 100}, {0, 100}},
			why:  "line 1 narrowed with margin 0; the rest full",
		},
		{
			name:   "a zero indent makes after irrelevant (:16689)",
			indent: 0, after: 3,
			want: []struct{ indent, width float64 }{{0, 100}, {0, 100}, {0, 100}},
			why:  "every line is hsize either way",
		},
		{
			name:   "after 0 hangs every line",
			indent: 10, after: 0,
			want: []struct{ indent, width float64 }{{10, 90}, {10, 90}, {10, 90}},
			why:  "last_special_line=0, so line 1 already takes the second shape",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := HangIndent(hsize, tc.indent, tc.after)
			for i, w := range tc.want {
				gotI, gotW := m.LineShape(i + 1)
				if gotI != w.indent || gotW != w.width {
					t.Errorf("line %d: indent=%v width=%v, want indent=%v width=%v (%s)",
						i+1, gotI, gotW, w.indent, w.width, tc.why)
				}
			}
		})
	}
}

func TestUniformIsEveryLineTheSame(t *testing.T) {
	m := Uniform(72)
	for _, n := range []int{1, 2, 99} {
		if i, w := m.LineShape(n); i != 0 || w != 72 {
			t.Errorf("line %d: indent=%v width=%v, want 0 and 72", n, i, w)
		}
	}
}

// ⛔ THE LAST PAIR REPEATS, which is why a Measure is an interface and not a
// slice: tex.web:16680-16681, "the specifications for line n apply to all
// subsequent lines".
func TestParShapeRepeatsItsLastPair(t *testing.T) {
	m := ParShape([]Shape{{Indent: 5, Width: 95}, {Indent: 10, Width: 90}})
	for _, tc := range []struct {
		line          int
		indent, width float64
	}{
		{1, 5, 95},
		{2, 10, 90},
		{3, 10, 90},  // beyond the list: the last pair
		{50, 10, 90}, // still the last pair
		{0, 5, 95},   // a line number below 1 cannot exist; clamp rather than panic
	} {
		if i, w := m.LineShape(tc.line); i != tc.indent || w != tc.width {
			t.Errorf("line %d: indent=%v width=%v, want %v and %v", tc.line, i, w, tc.indent, tc.width)
		}
	}
}

// \parshape=0 is how TeX turns the shape off; an empty list is not a usable
// measure and says so by giving nothing rather than pretending to a width.
func TestParShapeWithNoLines(t *testing.T) {
	if i, w := ParShape(nil).LineShape(1); i != 0 || w != 0 {
		t.Errorf("empty \\parshape: indent=%v width=%v, want 0 and 0 — the caller that means "+
			"\"no shape\" should pass Uniform", i, w)
	}
}

// ParShape copies, so a caller reusing its slice cannot reshape a paragraph
// that has already been broken.
func TestParShapeCopiesItsInput(t *testing.T) {
	pairs := []Shape{{Indent: 5, Width: 95}}
	m := ParShape(pairs)
	pairs[0] = Shape{Indent: 50, Width: 50}
	if i, w := m.LineShape(1); i != 5 || w != 95 {
		t.Errorf("mutating the caller's slice changed the measure: indent=%v width=%v", i, w)
	}
}

// ⭐⭐ THE POINT OF THE WHOLE FEATURE: the width is an input to every candidate
// break, so a hanging indent does not merely move lines — it BREAKS the
// paragraph somewhere else. If this engine only placed the indent afterwards,
// the two runs below would choose the same breaks.
func TestAHangingIndentChangesWhereTheParagraphBreaks(t *testing.T) {
	// Twelve equal words with ordinary interword glue, on a measure that fits
	// about four of them per line.
	// ⚠ BOTH SHAPES MUST BE FEASIBLE for the comparison to mean anything. The
	// first version of this used a 40pt indent and 2pt of glue stretch: the hung
	// paragraph then had NO solution within tolerance (three 20pt words need
	// +8pt on a 60pt line with 4pt to give), and the test failed saying so
	// instead of comparing two breakings. The glue is stretchier and the indent
	// smaller so that each shape has an answer and the answers differ.
	var items []Item
	for i := 0; i < 12; i++ {
		if i > 0 {
			items = append(items, Item{Kind: KGlue, Width: 4, Stretch: 6, Shrink: 2})
		}
		items = append(items, Item{Kind: KBox, Width: 20})
	}
	items = append(items,
		Item{Kind: KGlue, Width: 0, Stretch: 1, StretchOrder: 1}, // \parfillskip
		Item{Kind: KPenalty, Penalty: -InfPenalty})

	p := DefaultParams(400, 10)
	const hsize = 100

	flat, okFlat := KnuthPlassShaped(items, Uniform(hsize), p)
	if !okFlat {
		t.Fatal("the uniform control did not break: the witness is useless")
	}
	// A hanging indent on everything after line 1: 24 of 100 points gone, which
	// is about one word's worth and so moves the breaks without making the
	// paragraph impossible.
	hung, okHung := KnuthPlassShaped(items, HangIndent(hsize, 24, 1), p)
	if !okHung {
		t.Fatal("the hung paragraph did not break")
	}

	t.Logf("uniform: %d lines, ends %v", len(flat), ends(flat))
	t.Logf("hung:    %d lines, ends %v", len(hung), ends(hung))

	if len(flat) == len(hung) && sameEnds(flat, hung) {
		t.Error("the hanging indent changed nothing: the measure is not reaching the breaker, " +
			"which is the whole of go-typeset/linebreak#11")
	}
	// And the margin must travel with the lines that have it.
	if hung[0].Indent != 0 {
		t.Errorf("line 1 indent = %v, want 0 (hang_after=1 leaves the first line alone)", hung[0].Indent)
	}
	if len(hung) > 1 && hung[1].Indent != 24 {
		t.Errorf("line 2 indent = %v, want 24", hung[1].Indent)
	}
}

// The scalar entry point must keep behaving exactly as it did, since it is what
// every existing caller uses: it is now Uniform underneath and this pins that.
func TestTheScalarCallIsTheUniformShape(t *testing.T) {
	items := []Item{
		{Kind: KBox, Width: 30},
		{Kind: KGlue, Width: 5, Stretch: 3, Shrink: 1},
		{Kind: KBox, Width: 30},
		{Kind: KGlue, Width: 5, Stretch: 3, Shrink: 1},
		{Kind: KBox, Width: 30},
		{Kind: KGlue, Width: 0, Stretch: 1, StretchOrder: 1},
		{Kind: KPenalty, Penalty: -InfPenalty},
	}
	p := DefaultParams(200, 10)
	a, okA := KnuthPlassWith(items, 70, p)
	b, okB := KnuthPlassShaped(items, Uniform(70), p)
	if okA != okB || len(a) != len(b) {
		t.Fatalf("ok %v/%v, lines %d/%d", okA, okB, len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("line %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func ends(ls []Line) []int {
	out := make([]int, len(ls))
	for i, l := range ls {
		out[i] = l.End
	}
	return out
}

func sameEnds(a, b []Line) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].End != b[i].End {
			return false
		}
	}
	return true
}

func TestAbsOnNegativeAndPositive(t *testing.T) {
	if abs(-3) != 3 || abs(3) != 3 || abs(0) != 0 {
		t.Error("abs")
	}
	if math.Signbit(abs(-0.0)) {
		t.Error("abs(-0) should not keep the sign bit")
	}
}
