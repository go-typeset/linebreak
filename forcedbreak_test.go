package linebreak

import (
	"math"
	"testing"
)

// ⛔⛔ A FORCED BREAK IS MANDATORY, NOT MERELY TOLERATED.
//
// tex.web §16769:
//
//	if (b>inf_bad)or(pi=eject_penalty) then
//	  @<Prepare to deactivate node~|r| …@>
//
// Every active node is deactivated at an eject_penalty, so no path reaches past it.
// −InfPenalty used to mean only "this line may close even if it fits badly" (§16760's
// other branch), and the survivors were kept — so a solution that SPANNED the forced
// penalty stayed active and won on demerits at the end of the paragraph, where b == n
// is forced too.
//
// The visible effect, measured in go-tex/engine (which splits a paragraph at forced
// penalties to work around exactly this): with the split removed and the library
// unchanged, `un deux\penalty-10000 trois quatre` came out on ONE line, and the \hfil
// that LaTeX's \\ puts before the break stretched in the middle of it — "onze" at
// x=298.97 beside "neuf dix" instead of on the next line.
//
// ⛔ THE FIXTURE IS DELIBERATELY ONE THAT FITS. Four words of 2 on a measure of 100:
// the optimiser has every reason to keep them together, so a pass here means the break
// was taken because it is forced and not because it was convenient. A fixture whose
// words did not fit would break there anyway and the test would prove nothing.
//
// ⛔⛔ AND THE FIL BEFORE THE PENALTY IS NOT DECORATION — it is what LaTeX writes.
// `\\` is `\unskip \nobreak \hfil \break` (latex.ltx), so the line a forced break ends
// carries INFINITE stretch and is feasible at any tolerance. The fixture had no fil,
// which made its first line 5 wide on a measure of 100 with a stretch of 1: badness
// far past \tolerance=200. That passed only while this package recorded every forced
// break whatever its badness, which tex.web §16769 does not do ("else if b>threshold
// then goto deactivate"). With that corrected the fixture had to become the shape a
// consumer actually emits, or it would be testing the breaker on input no document
// produces.
func TestAForcedBreakIsTakenEvenWhenEverythingFits(t *testing.T) {
	items := withTerminator([]Item{
		Box(2), Glue(1, 1, 0.5), Box(2),
		Glue(0, 1e6, 0),                // the \hfil of LaTeX's \\
		Penalty(0, -InfPenalty, false), // the forced break, mid-paragraph
		Glue(1, 1, 0.5), Box(2), Glue(1, 1, 0.5), Box(2),
	})
	lines, ok := KnuthPlass(items, 100, 200, 10)
	if !ok {
		t.Fatal("aucune coupure recevable")
	}
	if len(lines) != 2 {
		t.Fatalf("lines=%d want 2: la pénalité forcée au milieu n'a pas coupé, "+
			"tout a tenu sur une ligne — %+v", len(lines), lines)
	}
	// The first line ends AT the forced penalty: item 4.
	if lines[0].End != 4 {
		t.Errorf("première ligne finit à %d, want 4 (la pénalité forcée)", lines[0].End)
	}
	// ⛔ And it is JUSTIFIED: the line is 5 wide against a measure of 100 and the fil
	// takes up the slack, so the ratio is positive and finite. This is the half the
	// engine could not get while a per-fragment \parfillskip absorbed the slack —
	// a ratio of 0 here would mean the line was set at its natural width.
	if lines[0].Ratio <= 0 || math.IsInf(lines[0].Ratio, 0) {
		t.Errorf("ratio de la première ligne = %v, want fini et > 0: une ligne "+
			"terminée par une coupure forcée se justifie comme une autre", lines[0].Ratio)
	}
}

// ⛔ THE CONTROL ON THE OTHER SIDE: a penalty that is NOT forced must still be
// optional. Deactivating at every penalty would pass the test above and destroy
// ordinary line breaking, so the same fixture with a finite penalty must stay on one
// line.
func TestAFinitePenaltyIsStillOptional(t *testing.T) {
	items := withTerminator([]Item{
		Box(2), Glue(1, 1, 0.5), Box(2),
		Penalty(0, 50, false), // a hint, not an order
		Glue(1, 1, 0.5), Box(2), Glue(1, 1, 0.5), Box(2),
	})
	lines, ok := KnuthPlass(items, 100, 200, 10)
	if !ok {
		t.Fatal("aucune coupure recevable")
	}
	if len(lines) != 1 {
		t.Errorf("lines=%d want 1: une pénalité finie de 50 ne doit pas forcer "+
			"une coupure quand tout tient — %+v", len(lines), lines)
	}
}

// ⛔ And a FORBIDDING penalty must not become a break either: +InfPenalty is the same
// magnitude with the other sign, and a comparison written on |pi| rather than on
// −|pi| would turn every \nobreak into a forced break.
func TestAForbiddingPenaltyIsNotAForcedBreak(t *testing.T) {
	items := withTerminator([]Item{
		Box(2), Glue(1, 1, 0.5), Box(2),
		Penalty(0, InfPenalty, false),
		Glue(1, 1, 0.5), Box(2), Glue(1, 1, 0.5), Box(2),
	})
	lines, ok := KnuthPlass(items, 100, 200, 10)
	if !ok {
		t.Fatal("aucune coupure recevable")
	}
	if len(lines) != 1 {
		t.Errorf("lines=%d want 1: +InfPenalty INTERDIT la coupure — %+v",
			len(lines), lines)
	}
}

// ⛔ Two forced breaks in a row give THREE lines, and the middle one is empty of words.
// It is here because the natural way to write the fix — emptying the survivor list —
// must still leave the node created AT the forced break itself, or a second forced
// penalty immediately after the first would find nothing active and the breaking would
// fail outright.
// ⛔ Each forced break is preceded by its fil, for the reason written on the test
// above: a line that a `\\` ends carries \hfil, and a forced break whose line is past
// the threshold is now deactivated rather than recorded (tex.web §16769).
func TestTwoForcedBreaksInARow(t *testing.T) {
	items := withTerminator([]Item{
		Box(2),
		Glue(0, 1e6, 0),
		Penalty(0, -InfPenalty, false),
		Box(2),
		Glue(0, 1e6, 0),
		Penalty(0, -InfPenalty, false),
		Box(2),
	})
	lines, ok := KnuthPlass(items, 100, 200, 10)
	if !ok {
		t.Fatal("aucune coupure recevable: la liste active a été vidée sans laisser " +
			"le nœud de la coupure forcée elle-même")
	}
	if len(lines) != 3 {
		t.Errorf("lines=%d want 3 — %+v", len(lines), lines)
	}
}
