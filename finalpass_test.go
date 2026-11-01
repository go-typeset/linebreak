package linebreak

import "testing"

// ⛔⛔ artificial_demerits, AND THE WHOLE POINT IS THAT THE COST IS ZERO.
//
// tex.web §16824-16831, the block §16769 jumps to:
//
//	if final_pass and (minimum_demerits=awful_bad) and
//	   (link(r)=last_active) and (prev_r=active) then
//	  artificial_demerits:=true {set demerits zero, this break is forced}
//	else if b>threshold then goto deactivate;
//
// and §16815-16821 says why it exists: "During the final pass, we dare not lose all
// active nodes… by permitting overfull boxes as a last resort", with Knuth's own
// warning to think thrice before touching it.
//
// Zero is what decides the outcome. A line past inf_bad costs 1e8 in demerits, so two
// hopeless short lines (2e8) otherwise LOSE to one overfull line (1e8) — and TeX picks
// the two short ones.
//
// ⛔ THE TEST IS THE SAME FIXTURE TWICE, flag off and flag on, because the flag's
// effect is the whole contract. Asserting only the flag-on answer would pass for an
// implementation that always breaks there, which is the error this clause is one line
// away from.
//
// Two unbreakable words of 35 with a RIGID space of 3 between them, on a measure of 40:
//
//	break at the space  → a line of 35 on 40, nothing to stretch: badness 10000,
//	                      infeasible at \tolerance=200
//	no break            → one line of 73 on 40, unshrinkable: overfull
//
// so neither answer is feasible and the choice is made on cost alone.
func TestFinalPassTakesAnInfeasibleBreakAtZeroCost(t *testing.T) {
	items := withTerminator([]Item{Box(35), Glue(3, 0, 0), Box(35)})
	p := DefaultParams(200, 10)

	// Flag off: the overfull single line wins on demerits.
	lines, ok := KnuthPlassWith(items, 40, p)
	if !ok {
		t.Fatal("sans FinalPass: aucune coupure recevable")
	}
	if len(lines) != 1 {
		t.Errorf("sans FinalPass: %d ligne(s), want 1 — la ligne débordante coûte 1e8 "+
			"et deux lignes désespérées 2e8: %+v", len(lines), lines)
	}

	// Flag on: the infeasible break is recorded at zero cost and now wins.
	q := p
	q.FinalPass = true
	lines, ok = KnuthPlassWith(items, 40, q)
	if !ok {
		t.Fatal("avec FinalPass: aucune coupure recevable")
	}
	if len(lines) != 2 {
		t.Fatalf("avec FinalPass: %d ligne(s), want 2 — artificial_demerits doit "+
			"enregistrer la coupure au coût NUL: %+v", len(lines), lines)
	}
	// The break is at the space, item 1.
	if lines[0].End != 1 {
		t.Errorf("première ligne finit à %d, want 1 (l'espace)", lines[0].End)
	}

	// ⛔ AND THE CLAUSE MUST NOT FIRE WHERE SOMETHING IS FEASIBLE. Same words, measure
	// 35: the break after the first word is an EXACT fit, so it is recorded normally
	// and the answer is the same with the flag either way. If the flag changed this,
	// it would be firing on feasible breakpoints — which is how it hyphenated titles
	// in go-tex/engine when it was armed on a pass that had not failed.
	exact := withTerminator([]Item{Box(35), Glue(3, 0, 0), Box(35)})
	a, okA := KnuthPlassWith(exact, 35, p)
	b, okB := KnuthPlassWith(exact, 35, q)
	if !okA || !okB {
		t.Fatal("ajustement exact: aucune coupure recevable")
	}
	if len(a) != len(b) || len(a) != 2 {
		t.Errorf("ajustement exact: %d ligne(s) sans le drapeau, %d avec, want 2 et 2 — "+
			"la clause ne doit rien changer là où une coupure est recevable",
			len(a), len(b))
	}
}
