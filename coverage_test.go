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
	a := breakNode{fitness: decent}
	items := []Item{Box(1)}
	pr := DefaultParams(200, 10)
	neutral := demerits(0, decent, 0, false, &a, items, false, pr)
	discouraged := demerits(0, decent, 50, false, &a, items, false, pr)
	encouraged := demerits(0, decent, -50, false, &a, items, false, pr)
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
	afterFlagged := &breakNode{position: 1, fitness: decent}
	afterBox := &breakNode{position: 0, fitness: decent}
	pr := DefaultParams(200, 10)
	with := demerits(0, decent, 50, true, afterFlagged, items, false, pr)
	without := demerits(0, decent, 50, true, afterBox, items, false, pr)
	if with-without != 10000 {
		t.Errorf("consecutive flagged penalty = %v, want 10000", with-without)
	}
}

// TeX sorts each line into one of four fitness classes by how hard its spaces
// worked (tex.web §16790-16812), and charges \adjdemerits when two adjacent
// lines sit more than one class apart — a very loose line under a tight one
// reads as a hole in the page. The classes are cut on BADNESS, not on the ratio:
// badness 12 is a ratio of about a half, badness 99 about one.
func TestFitnessClasses(t *testing.T) {
	for _, c := range []struct {
		nom  string
		r    float64
		want int
	}{
		{"serré", -0.9, tight},
		{"à peine serré", -0.3, decent},
		{"juste", 0, decent},
		{"à peine lâche", 0.4, decent},
		{"lâche", 0.8, loose},
		{"très lâche", 1.5, veryLoose},
	} {
		if got := fitness(c.r); got != c.want {
			t.Errorf("fitness(%v) [%s] = %d, want %d", c.r, c.nom, got, c.want)
		}
	}
}

// Two lines two classes apart cost \adjdemerits more than two neighbouring ones.
func TestAdjacentIncompatibleLinesCost(t *testing.T) {
	pr := DefaultParams(200, 10)
	items := []Item{Box(1)}
	fromTight := &breakNode{fitness: tight}
	fromLoose := &breakNode{fitness: loose}
	// A very loose line after a tight one is three classes away; after a loose
	// one, one class.
	far := demerits(1.5, veryLoose, 0, false, fromTight, items, false, pr)
	near := demerits(1.5, veryLoose, 0, false, fromLoose, items, false, pr)
	if far-near != pr.AdjDemerits {
		t.Errorf("écart de classes: %v de plus, want %v", far-near, pr.AdjDemerits)
	}
}

// A hyphen on the LAST line but one costs \finalhyphendemerits, which is less
// than the \doublehyphendemerits charged in the middle of a paragraph: TeX would
// rather hyphenate there than leave the paragraph ragged.
func TestFinalHyphenCostsLessThanADoubleOne(t *testing.T) {
	pr := DefaultParams(200, 10)
	items := []Item{Box(1), Penalty(0, 50, true), Box(1)}
	after := &breakNode{position: 1, fitness: decent}
	mid := demerits(0, decent, 50, true, after, items, false, pr)
	end := demerits(0, decent, 50, true, after, items, true, pr)
	if mid-end != pr.DoubleHyph-pr.FinalHyph {
		t.Errorf("dernière ligne: %v de moins, want %v", mid-end, pr.DoubleHyph-pr.FinalHyph)
	}
}

// Past badness 10000 a line is hopeless, not merely bad: TeX stops squaring and
// charges a flat ceiling (tex.web §16902).
func TestHopelessLineHitsTheCeiling(t *testing.T) {
	pr := DefaultParams(MaxBadRatio, 10)
	items := []Item{Box(1)}
	a := &breakNode{fitness: decent}
	if got := demerits(MaxBadRatio, veryLoose, 0, false, a, items, false, pr); got < 1e8 {
		t.Errorf("ligne désespérée = %v, want au moins le plafond 1e8", got)
	}
}
