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
//
// ⛔⛔ AND THE ANSWER IS THE REFERENCE'S, NOT AN ARGUMENT. This file used to assert
// the opposite of both lines below — one line with the flag OFF, two with it ON — and
// the fixture was never put to TeX. It has been now. The same two boxes, the same
// rigid space and the same 40pt measure, through tectonic 0.17.0 with
// \tolerance=200, \emergencystretch=0pt, \pretolerance=-1:
//
//	Overfull \hbox (33.0pt too wide) in paragraph at lines 23--23
//	\hbox(0.4+0.0)x40.0
//	.\hbox(0.4+0.0)x35.0, glue set 35.0fill []
//	.\glue 3.0
//	.\hbox(0.4+0.0)x35.0, glue set 35.0fill []
//
// ONE line, holding both boxes and the space, 33pt past the margin. Not two.
//
// Why: at the space, the line wants 5pt from a glue with no stretch, so tex.web §108
// gives b = inf_bad = 10000 — which is NOT `b > inf_bad`, so §16769 takes its OTHER
// branch, "goto continue": the break is not recorded and the node stays active, on
// the final pass like any other. The artificial clause is reached only where the line
// is hopeless (b > inf_bad, our r < −1) or the break is forced, and here that is the
// paragraph's own end.
//
// ⚠ \emergencystretch=0pt in the witness is not a detail: §863 sets
// `final_pass := (emergency_stretch<=0)` on the SECOND pass, so with no emergency
// stretch TeX's second pass IS its final one and the clause is live there.
func TestFinalPassTakesAnInfeasibleBreakAtZeroCost(t *testing.T) {
	items := withTerminator([]Item{Box(35), Glue(3, 0, 0), Box(35)})
	p := DefaultParams(200, 10)

	// Flag off: nothing at all is feasible, so the pass FAILS and the caller must
	// run the next one. It used to return the overfull line here, by recording the
	// forced break at the paragraph's end whatever its badness — go-typeset/linebreak#12
	// item (5), and the reason a consumer had to test a successful pass for bad lines.
	if lines, ok := KnuthPlassWith(items, 40, p); ok {
		t.Errorf("sans FinalPass: ok=true, %d ligne(s) — une passe qui ne peut pas "+
			"composer dans \\tolerance doit ÉCHOUER: %+v", len(lines), lines)
	}

	// Flag on: the paragraph's forced end is hopeless, nothing has been recorded and
	// the start node is the only active one, so the break is taken at zero cost — the
	// reference's single overfull line.
	q := p
	q.FinalPass = true
	lines, ok := KnuthPlassWith(items, 40, q)
	if !ok {
		t.Fatal("avec FinalPass: aucune coupure recevable")
	}
	if len(lines) != 1 {
		t.Fatalf("avec FinalPass: %d ligne(s), want 1 — tectonic 0.17.0 en compose UNE, "+
			"débordante de 33.0pt: %+v", len(lines), lines)
	}
	if lines[0].Ratio >= -1 {
		t.Errorf("ratio=%v, want < -1: la ligne de 73 sur 40 ne peut pas se serrer "+
			"et c'est ce débordement que la clause accepte", lines[0].Ratio)
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

// ⛔⛔ AND IT MUST NOT FIRE AT AN ORDINARY BREAKPOINT THAT IS MERELY PAST THE
// THRESHOLD. This is the other half of §16769's branch, and the half that cost
// go-tex/engine eleven hyphenated titles ("THETI-TLE") when it armed FinalPass.
//
// The clause was written outside the branch, so on a final pass it fired wherever
// nothing had yet been recorded — including at a glue whose line is too short to be
// feasible but not hopeless (b > threshold, b <= inf_bad). tex.web sends those to
// `goto continue`: do not record, keep the node active. Recorded at ZERO cost they
// beat every real break, because every real break costs something.
//
// The fixture separates the two cases that §16769 treats differently, which the
// fixture above cannot: two unbreakable boxes of 60 with a STRETCHABLE space of
// 2 (plus 1, minus 0.5) on a measure of 100.
//
//	break at the space → 60 on 100, wanting 40 from a stretch of 1. §108 caps
//	                     badness at inf_bad, so b = 10000: past \tolerance, and
//	                     NOT past inf_bad → "goto continue"
//	no break           → 122 on 100 with 0.5 of shrink: overfull, b = awful_bad
//	                     → the branch, and the clause
//
// ⛔ THE ANSWER IS THE REFERENCE'S. tectonic 0.17.0 on the same shape, same
// \tolerance=200 and \emergencystretch=0pt:
//
//	Overfull \hbox (21.5pt too wide) in paragraph at lines 27--27
//	\hbox(0.4+0.0)x100.0, glue set - 1.0
//	.\hbox(0.4+0.0)x60.0, glue set 60.0fill []
//	.\glue 2.0 plus 1.0 minus 0.5
//	.\hbox(0.4+0.0)x60.0, glue set 60.0fill []
//
// ONE line, both boxes on it, 21.5pt past the margin — on a pass that is final
// (no emergency stretch). TeX would rather overfull than take the break at that
// space, and the clause is why it may do the first and not the second.
//
// ⚠ AND IT BITES: with the branch test removed the clause fires at the space, that
// break is recorded at zero cost, and the node it creates means the paragraph's end
// no longer has a single active node — so the overfull line is never recorded and
// the answer comes back as TWO lines. Checked by mutation, not assumed.
func TestFinalPassDoesNotFireWhereTeXSaysContinue(t *testing.T) {
	fixture := func() []Item {
		return withTerminator([]Item{Box(60), Glue(2, 1, 0.5), Box(60)})
	}
	p := DefaultParams(200, 10)
	q := p
	q.FinalPass = true

	if off, okOff := KnuthPlassWith(fixture(), 100, p); okOff {
		t.Errorf("sans FinalPass: ok=true, %d ligne(s) — rien n'est recevable: %+v",
			len(off), off)
	}
	on, okOn := KnuthPlassWith(fixture(), 100, q)
	if !okOn {
		t.Fatal("avec FinalPass: aucune coupure recevable")
	}
	if len(on) != 1 {
		t.Fatalf("avec FinalPass: %d ligne(s), want 1 — la clause a enregistré la "+
			"coupure ORDINAIRE de l'espace au coût NUL, là où tex.web dit "+
			"« goto continue »; tectonic en compose UNE, débordante de 21.5pt: %+v",
			len(on), on)
	}
	if on[0].Ratio >= -1 {
		t.Errorf("ratio=%v, want < -1: c'est la ligne débordante que la clause accepte",
			on[0].Ratio)
	}
}
