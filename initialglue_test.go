package linebreak

import "testing"

// ⛔⛔ A PARAGRAPH'S OPENING GLUE BELONGS TO ITS FIRST LINE.
//
// The active list is seeded with {position: 0}, the paragraph's beginning, and
// measure() used to treat a glue there as "the glue we broke at" and skip it. Position
// 0 is not a break: tex.web:17001 is `prev_p:=cur_p; {glue at beginning is not a legal
// breakpoint}`, and legal() refuses it for exactly that reason. Discarding it anyway
// cost a paragraph its opening glue — go-tex/engine#657, where \noindent\hskip 20pt
// lost all 20pt, the reference starting the line at x=153.69 and the engine at 133.77.
//
// Both halves are asserted, because the same test is made in two places: Start comes
// from the line reconstruction and Ratio from measure(), and fixing one alone would
// report a line whose width was computed over a different span than the one it names.
func TestOpeningGlueStaysInTheFirstLine(t *testing.T) {
	// glue(20) box(30) glue(10) box(30) parfill: the glue at index 0 follows nothing
	// and is not a legal break; the one at index 2 follows a box and is. The closing
	// glue is TeX's \parfillskip — without it the last line cannot stretch, the break
	// at index 2 is infeasible, and the breaker puts everything on one line, which is
	// what the first draft of this fixture measured.
	items := []Item{
		Glue(20, 0, 0),
		Box(30),
		Glue(10, 5, 5),
		Box(30),
		Glue(0, 1e6, 0),
	}
	lines, ok := KnuthPlass(items, 50, 10000, 10)
	if !ok {
		t.Fatal("aucune coupe trouvée")
	}
	if len(lines) == 0 {
		t.Fatal("aucune ligne")
	}
	if lines[0].Start != 0 {
		t.Errorf("la 1re ligne commence à %d, want 0: la colle d'ouverture lui "+
			"appartient (tex.web:17001)", lines[0].Start)
	}

	// ⛔ And the WIDTH must agree with that Start. The first line is glue(20) +
	// box(30) = 50 against a 50-wide measure, so it is exactly full and its ratio is
	// 0. Measured over [1,2) instead it would be 30 wide and stretch, which is the
	// half a Start-only fix would leave behind.
	if r := lines[0].Ratio; r < -1e-9 || r > 1e-9 {
		t.Errorf("ratio de la 1re ligne = %g, want 0: largeur mesurée sur [%d,%d) "+
			"doit valoir 20+30 = 50", r, lines[0].Start, lines[0].End)
	}
}

// ⛔ The control: glue that really does follow a break is still discarded, or the fix
// would have traded one wrong width for another. Three boxes of 30 with 10-wide glue
// between them and a measure of 30 forces a break at each glue.
func TestGlueAtARealBreakIsStillDiscarded(t *testing.T) {
	items := []Item{
		Box(30),
		Glue(10, 0, 0),
		Box(30),
		Glue(10, 0, 0),
		Box(30),
	}
	lines, ok := KnuthPlass(items, 30, 10000, 10)
	if !ok || len(lines) < 2 {
		t.Fatalf("want au moins 2 lignes, got %d (ok=%v)", len(lines), ok)
	}
	if lines[0].Start != 0 {
		t.Errorf("1re ligne: Start = %d, want 0", lines[0].Start)
	}
	// The second line must begin AFTER the glue it broke at, not on it.
	if s := lines[1].Start; s != 2 {
		t.Errorf("2e ligne: Start = %d, want 2 — la colle de la coupe est jetée", s)
	}
}
