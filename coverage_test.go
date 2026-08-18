package linebreak

import "testing"

// Glyph carries a rune and its vertical metrics: it is the box a typesetter makes
// for one character, as against the metric-only Box.
func TestGlyphCarriesRuneAndMetrics(t *testing.T) {
	it := Glyph('é', 4.5, 7, 2)
	if it.Kind != KBox {
		t.Errorf("Glyph kind = %v, want KBox", it.Kind)
	}
	if it.R != 'é' || it.Width != 4.5 || it.Height != 7 || it.Depth != 2 {
		t.Errorf("Glyph = %+v, want rune é and 4.5/7/2", it)
	}
}

// No feasible set of breaks: every line would be far worse than the tolerance
// allows, and the caller is told so rather than handed a bad paragraph.
func TestNoFeasibleBreaksReportsFailure(t *testing.T) {
	// A single box and no breakpoint at all: there is no sequence of breaks to find.
	// By the second legal break the line is overfull BEYOND its shrink (r < -1), so
	// the only active node is dropped; the forced break at the end then has nothing
	// to attach to and no set of breaks exists.
	items := []Item{Box(500), Glue(0, 0, 1), Box(1), Glue(0, 0, 0), Box(1)}
	if lines, ok := KnuthPlass(items, 10, 1, 10); ok {
		t.Errorf("a paragraph with no breakpoint reported success: %v", lines)
	}
}

// A break penalty pulls the demerits in the direction of its sign: a POSITIVE
// penalty (a discouraged break) costs more than the same break with none, and a
// NEGATIVE one (an encouraged break) costs less.
func TestPenaltySignMovesDemerits(t *testing.T) {
	var a breakNode
	items := []Item{Box(1)}
	neutral := demerits(0, 0, false, &a, items, 10)
	discouraged := demerits(0, 50, false, &a, items, 10)
	encouraged := demerits(0, -50, false, &a, items, 10)
	if !(discouraged > neutral) {
		t.Errorf("a positive penalty did not cost more: %v vs %v", discouraged, neutral)
	}
	if !(encouraged < neutral) {
		t.Errorf("a negative penalty did not cost less: %v vs %v", encouraged, neutral)
	}
}

// Two flagged breaks in a row (a hyphen ending consecutive lines) carry TeX's
// extra penalty; the same break not preceded by a flagged one does not.
func TestConsecutiveFlaggedBreaksArePenalised(t *testing.T) {
	items := []Item{Box(1), Penalty(0, 50, true), Box(1)}
	afterFlagged := &breakNode{position: 1}
	afterBox := &breakNode{position: 0}
	with := demerits(0, 50, true, afterFlagged, items, 10)
	without := demerits(0, 50, true, afterBox, items, 10)
	if with-without != 10000 {
		t.Errorf("consecutive flagged penalty = %v, want 10000", with-without)
	}
}
