package linebreak

import (
	"math"
	"testing"
)

// ⛔⛔ AN INFINITE ORDER IS A CLASS, NOT A LARGE NUMBER.
//
// tex.web §16790-16793, which is the whole of this file:
//
//	if (cur_active_width[3]<>0)or(cur_active_width[4]<>0)or(cur_active_width[5]<>0)
//	  then begin b:=0; fit_class:=decent_fit; {infinite stretch} end
//	else … b:=badness(shortfall,cur_active_width[2]) …
//
// No badness is computed when any fil/fill/filll is present: the line is
// PERFECT however short it is. A consumer that flattens `plus 1fil` into the
// finite column gets a large but finite badness instead, which is
// go-typeset/linebreak#12 item (1) and the reason go-tex/engine had to open the
// tolerance by hand to make \raggedright behave.
func TestInfiniteStretchIsFeasibleAtAnyTolerance(t *testing.T) {
	// Three short words on a wide measure. Justified, each line is hopeless:
	// 10 wide on a measure of 300 with 1 of stretch is a ratio of 290.
	items := func(order uint8) []Item {
		return withTerminator([]Item{
			Box(10), GlueOrder(1, 1, order, 0.5), Box(10),
			Penalty(0, -InfPenalty, false), // force a break so a short line must be set
			GlueOrder(1, 1, order, 0.5), Box(10),
		})
	}
	p := DefaultParams(200, 10)

	// OrderPt: the first line is 10 wide on 300 with 1 of finite stretch, so its
	// badness is far past \tolerance=200 and the pass must FAIL.
	if lines, ok := KnuthPlassWith(items(OrderPt), 300, p); ok {
		t.Errorf("fini: ok=true, %d ligne(s) — une ligne de 10 sur 300 avec 1 de "+
			"ressort fini est infaisable à \\tolerance=200: %+v", len(lines), lines)
	}
	// OrderFil: the SAME paragraph, the same amount of stretch, one order up.
	// Every line now has badness 0 and the pass succeeds.
	lines, ok := KnuthPlassWith(items(OrderFil), 300, p)
	if !ok {
		t.Fatal("fil: aucune coupure recevable — §16790 donne b=0 à toute ligne " +
			"portant du fil, donc elle est recevable à n'importe quelle tolérance")
	}
	if len(lines) != 2 {
		t.Fatalf("fil: %d ligne(s), want 2 (la pénalité forcée coupe): %+v", len(lines), lines)
	}
	for i, ln := range lines {
		if ln.StretchOrder != OrderFil {
			t.Errorf("fil: ligne %d d'ordre %d, want OrderFil — c'est l'ordre le plus "+
				"haut présent qui prend le mou", i, ln.StretchOrder)
		}
	}
}

// ⛔ AND THE TOLERANCE IS NOT WHAT CARRIES IT. The test above would also pass if
// an infinite order merely multiplied the stretch by a huge number, so here the
// tolerance is driven to its floor: at \tolerance=0 only a PERFECT line is
// feasible, and a fil line is perfect.
func TestInfiniteStretchIsFeasibleAtToleranceZero(t *testing.T) {
	items := withTerminator([]Item{Box(10), GlueOrder(0, 1, OrderFil, 0), Box(10)})
	lines, ok := KnuthPlassWith(items, 300, DefaultParams(0, 10))
	if !ok {
		t.Fatal("aucune coupure recevable à \\tolerance=0: une ligne portant du fil " +
			"a la badness 0, pas une petite badness")
	}
	if len(lines) != 1 {
		t.Fatalf("%d ligne(s), want 1 — tout tient: %+v", len(lines), lines)
	}
}

// ⛔ FIL BEATS ANY AMOUNT OF POINTS, which is what "order" means. A line
// carrying 1 fil and a billion points of finite stretch is stretched by its
// FIL: the finite glue stays at its natural width.
func TestTheHighestOrderTakesTheSlackAlone(t *testing.T) {
	for _, c := range []struct {
		name  string
		y     [numOrders]float64
		want  uint8
		ratio float64 // (L-w)/y[want] with w=100, L=200
	}{
		{"fini seul", [numOrders]float64{4, 0, 0, 0}, OrderPt, 25},
		{"fil contre un milliard de points", [numOrders]float64{1e9, 4, 0, 0}, OrderFil, 25},
		{"fill contre fil", [numOrders]float64{1e9, 1e9, 4, 0}, OrderFill, 25},
		{"filll contre fill", [numOrders]float64{1e9, 1e9, 1e9, 4}, OrderFilll, 25},
	} {
		r, order := ratio(100, c.y, 0, 200)
		if order != c.want {
			t.Errorf("%s: ordre %d, want %d", c.name, order, c.want)
		}
		if math.Abs(r-c.ratio) > 1e-9 {
			t.Errorf("%s: ratio %v, want %v — mesuré contre la colonne de l'ordre "+
				"le plus haut, pas contre la somme", c.name, r, c.ratio)
		}
	}
}

// ⛔ SHRINKING HAS NO ORDERS, and tex.web says why in its own words (§16806):
// "Shrinkability is never infinite in a paragraph". An overfull line carrying
// fil is still overfull — fil can only pull a line wider — so it reports
// OrderPt and a negative ratio, and the `r >= -1` predicate every caller writes
// must still refuse it.
func TestFilDoesNotRescueAnOverfullLine(t *testing.T) {
	var y [numOrders]float64
	y[OrderFil] = 1 // all the fil in the world
	r, order := ratio(300, y, 0, 100)
	if order != OrderPt {
		t.Errorf("ordre %d, want OrderPt: une ligne qu'on serre n'a pas d'ordre infini", order)
	}
	if !math.IsInf(r, -1) {
		t.Errorf("ratio %v, want -Inf: trop longue et aucun retrait", r)
	}
	if r >= -1 {
		t.Error("r >= -1 sur une ligne débordante: le prédicat des appelants la laisserait passer")
	}
	if got := lineBadness(r, order); got != InfPenalty {
		t.Errorf("badness %v, want %v: le fil ne rend pas une ligne serrée bonne", got, InfPenalty)
	}
}

// ⛔⛔ THE BACKGROUND IS PER LINE, AND IT IS WHAT \raggedright IS.
//
// tex.web §16307-16315 seeds active_width with \leftskip+\rightskip before
// walking any item, so EVERY line carries it. \raggedright is
// \rightskip = 0pt plus 1fil: every line gets fil, every line has badness 0,
// and TeX's first pass — which has no hyphenation at all — succeeds outright.
//
// The fixture is a paragraph that CANNOT be set justified at this tolerance and
// has no discretionaries to fall back on, so the only thing that can make it
// feasible is the background.
func TestRaggedRightBackgroundMakesEveryLineFeasible(t *testing.T) {
	items := withTerminator([]Item{
		Box(10), Glue(1, 1, 0.5), Box(10),
		Penalty(0, -InfPenalty, false),
		Glue(1, 1, 0.5), Box(10),
	})
	justified := DefaultParams(200, 10)
	if lines, ok := KnuthPlassWith(items, 300, justified); ok {
		t.Errorf("justifié: ok=true, %d ligne(s) — ce paragraphe n'est pas composable "+
			"à \\tolerance=200: %+v", len(lines), lines)
	}

	ragged := justified
	ragged.Background.Stretch[OrderFil] = 1 // \rightskip = 0pt plus 1fil
	lines, ok := KnuthPlassWith(items, 300, ragged)
	if !ok {
		t.Fatal("\\raggedright: aucune coupure recevable — le fond doit donner du fil " +
			"à CHAQUE ligne")
	}
	if len(lines) != 2 {
		t.Fatalf("\\raggedright: %d ligne(s), want 2: %+v", len(lines), lines)
	}
	// ⛔ EVERY line, not just one: a background added once would leave the other
	// line finite, and this is the assertion that catches it.
	for i, ln := range lines {
		if ln.StretchOrder != OrderFil {
			t.Errorf("ligne %d d'ordre %d, want OrderFil: le fond n'a pas été ajouté "+
				"à cette ligne", i, ln.StretchOrder)
		}
	}
}

// ⛔ AND THE BACKGROUND'S WIDTH NARROWS THE MEASURE. \leftskip and \rightskip
// take their width from every line before any item is placed (§16307-16315,
// `background[1]:=width(q)+width(r)`), so a 20pt \leftskip on a 100pt measure
// leaves 80pt for the text — which is go-tex/engine#583 as the library sees it.
func TestBackgroundWidthNarrowsEveryLine(t *testing.T) {
	// Two boxes of 40 and a rigid space of 5: 85 natural. On a measure of 100 it
	// fits; with a 20-wide background it does not and must break.
	items := withTerminator([]Item{Box(40), Glue(5, 0, 0), Box(40)})
	wide := DefaultParams(MaxBadRatio, 10) // tolerant: the question is the WIDTH
	lines, ok := KnuthPlassWith(items, 100, wide)
	if !ok || len(lines) != 1 {
		t.Fatalf("sans fond: ok=%v, %d ligne(s), want 1 — 85 tient dans 100", ok, len(lines))
	}
	narrow := wide
	narrow.Background.Width = 20
	lines, ok = KnuthPlassWith(items, 100, narrow)
	if !ok {
		t.Fatal("avec un fond de 20: aucune coupure recevable")
	}
	if len(lines) != 2 {
		t.Fatalf("avec un fond de 20: %d ligne(s), want 2 — 85 ne tient pas dans 80, "+
			"et le fond est compté sur chaque ligne: %+v", len(lines), lines)
	}
}

// ⚠ A ZERO Background AND A ZERO StretchOrder CHANGE NOTHING, which is what
// lets every caller written before this exist unchanged. The whole suite is
// that assertion; this states it once, deliberately, so a future reader knows
// it was a requirement and not a coincidence.
func TestTheZeroValuesAreThePreviousBehaviour(t *testing.T) {
	items := para(6, 2, 1, 1, 0.5)
	a, okA := KnuthPlass(items, 8, 200, 10)
	p := DefaultParams(200, 10) // zero Background
	b, okB := KnuthPlassWith(items, 8, p)
	if !okA || !okB || len(a) != len(b) {
		t.Fatalf("ok %v/%v, %d vs %d ligne(s)", okA, okB, len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("ligne %d: %+v vs %+v", i, a[i], b[i])
		}
		if a[i].StretchOrder != OrderPt {
			t.Errorf("ligne %d: ordre %d, want OrderPt pour de la glue finie",
				i, a[i].StretchOrder)
		}
	}
}

// ⚠ An order above filll is clamped rather than indexing past the array: a
// caller that computes an order arithmetically must not be able to panic the
// breaker.
func TestAnOrderAboveFilllIsClamped(t *testing.T) {
	it := GlueOrder(0, 1, 99, 0)
	if it.StretchOrder != OrderFilll {
		t.Errorf("ordre %d, want OrderFilll (serré)", it.StretchOrder)
	}
	// and the same through the field, which the prefix sums must survive
	items := withTerminator([]Item{Box(10), Item{Kind: KGlue, Stretch: 1, StretchOrder: 99}, Box(10)})
	if _, ok := KnuthPlassWith(items, 300, DefaultParams(200, 10)); !ok {
		t.Error("un ordre hors bornes posé à la main a fait échouer la composition")
	}
}
