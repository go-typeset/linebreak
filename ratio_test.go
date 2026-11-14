package linebreak

import (
	"math"
	"testing"
)

// ⛔⛔ THE SIGN OF THE RATIO IS THE CONTRACT, AND AN OVERFULL LINE MUST BE NEGATIVE.
//
// Every caller asks `r >= -1` to mean "this line is not overfull past its shrink",
// and that predicate is the only thing standing between an impossible line and the
// active list. An unshrinkable overfull line used to come back +Inf — "impossibly
// bad", which is true, and on the wrong side of the scale: it passed `r >= -1` and
// read as an infinitely STRETCHED line, so its node stayed active and could still be
// chosen.
//
// tex.web §16767-16769 is the other way round: shrinking past the available shrink
// gives b := inf_bad+1, and `if (b>inf_bad)…` then deactivates the node. Overfull
// means out, not loose.
//
// ⛔ This is a UNIT test on ratio, because nothing above it saw the mutation. With
// the sign flipped back to +Inf the whole package suite stayed green — the sign only
// changes which node survives, and no fixture had a case where that decided the
// outcome. The function's contract is the level the sign lives at, so this is where
// it is asserted, together with the predicate the callers actually write.
func TestRatioSignsSayOverfullFromLoose(t *testing.T) {
	for _, c := range []struct {
		name          string
		w, y, z, L    float64
		wantSign      int // -1 négatif, 0 nul, +1 positif
		wantPassesMin bool
	}{
		// Overfull with no shrink at all: the case that was +Inf.
		{"trop long, aucun retrait", 100, 0, 0, 10, -1, false},
		{"trop long, aucun retrait, de peu", 11, 0, 0, 10, -1, false},
		// Overfull beyond its shrink: already negative, and already excluded.
		{"trop long au-delà du retrait", 100, 0, 1, 10, -1, false},
		// Overfull but within its shrink: negative, and ALLOWED (r ≥ −1).
		{"trop long mais absorbable", 11, 0, 2, 10, -1, true},
		// Underfull with no stretch: bad but FINITE and positive, and the node stays —
		// tex.web gives it b := inf_bad, which is not > inf_bad, so no deactivation.
		{"trop court, aucun étirement", 5, 0, 0, 10, 1, true},
		{"trop court, étirable", 5, 10, 0, 10, 1, true},
		{"juste", 10, 1, 1, 10, 0, true},
	} {
		// ⚠ Every case here is FINITE glue, so the stretch goes in column 0 and
		// the order comes back OrderPt. The infinite orders have their own tests:
		// what this one is about is the SIGN, which no order changes.
		r, order := ratio(c.w, [numOrders]float64{c.y}, c.z, c.L)
		if order != OrderPt {
			t.Errorf("%s: ordre %d, want OrderPt — ce cas n'a que du ressort fini",
				c.name, order)
		}
		var sign int
		switch {
		case r < 0:
			sign = -1
		case r > 0:
			sign = 1
		}
		if sign != c.wantSign {
			t.Errorf("%s: ratio(%v,%v,%v,%v) = %v, signe %d want %d",
				c.name, c.w, c.y, c.z, c.L, r, sign, c.wantSign)
		}
		if got := r >= -1; got != c.wantPassesMin {
			t.Errorf("%s: ratio = %v, (r >= -1) = %v want %v — c'est ce prédicat qui "+
				"décide si le nœud reste actif", c.name, r, got, c.wantPassesMin)
		}
		// ⛔ And the COST must be unchanged by the sign: badness answers InfPenalty for
		// either infinity, so this fix moves which node survives and not how much a
		// line costs. Asserting it keeps the two apart.
		if math.IsInf(r, 0) && badness(r) != InfPenalty {
			t.Errorf("%s: badness(%v) = %v want InfPenalty", c.name, r, badness(r))
		}
	}
	// The underfull-no-stretch sentinel is deliberately FINITE: a node with nothing to
	// stretch is still usable, and making it infinite would exclude it like an overfull
	// one. If MaxBadRatio ever became infinite this would say so.
	if math.IsInf(MaxBadRatio, 0) {
		t.Error("MaxBadRatio est infini: une ligne trop courte sans étirement " +
			"serait exclue comme une ligne débordante")
	}
}
