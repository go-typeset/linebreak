// Copyright (c) the go-typeset authors.
// SPDX-License-Identifier: BSD-3-Clause

package linebreak

import (
	"math"
	"testing"
)

// \emergencystretch, issue #12 finding (6): "§863 adds it to background[2] for
// the final pass. The consumer raises the tolerance to 1e4 instead
// (paragraph.go:251-258), which is a different algorithm with a different
// answer."
//
// The paragraph below is deliberately impossible at its measure: two 40pt boxes
// and one 4pt glue carrying 1pt of stretch, set to 100pt. Natural width is 84,
// so the line needs +16pt and has 1pt to give — ratio 16, badness 100·16³,
// four orders past inf_bad.
//
// ⛔ THE WITNESS IS THE RATIO, NOT THE BOOLEAN. Both arms run with FinalPass
// set, because that is when §863 applies at all, and FinalPass is precisely the
// flag that makes a hopeless break succeed anyway through artificial_demerits.
// So "did it set?" cannot separate them: both do. What separates them is how
// badly.
func emergencyItems() []Item {
	return []Item{
		{Kind: KBox, Width: 40},
		{Kind: KGlue, Width: 4, Stretch: 1, Shrink: 0.5},
		{Kind: KBox, Width: 40},
	}
}

const emergencyMeasure = 100.0

func TestEmergencyStretchReachesTheLineAsFiniteStretch(t *testing.T) {
	p := DefaultParams(200, 10)
	p.FinalPass = true

	without, ok := KnuthPlassWith(emergencyItems(), emergencyMeasure, p)
	if !ok || len(without) != 1 {
		t.Fatalf("without: ok=%v lines=%d, want one line (artificial_demerits forces it)", ok, len(without))
	}

	p.EmergencyStretch = 15 // 1 + 15 = the 16pt the line is short
	with, ok := KnuthPlassWith(emergencyItems(), emergencyMeasure, p)
	if !ok || len(with) != 1 {
		t.Fatalf("with: ok=%v lines=%d, want one line", ok, len(with))
	}

	// 16pt short over 16pt of stretch is exactly ratio 1.
	if got := with[0].Ratio; math.Abs(got-1) > 1e-9 {
		t.Errorf("ratio with \\emergencystretch = %v, want 1 (16pt short, 1+15pt of stretch)", got)
	}
	// Without it, the same line is stretched sixteen times its glue.
	if got := without[0].Ratio; math.Abs(got-16) > 1e-9 {
		t.Errorf("ratio without = %v, want 16 (16pt short, 1pt of stretch)", got)
	}
	// ⚠ FINITE, not fil: an infinite order would make every line badness 0 and
	// the emergency pass would stop telling breaks apart.
	if with[0].StretchOrder != OrderPt {
		t.Errorf("StretchOrder = %v, want OrderPt: \\emergencystretch is background[2], the finite slot", with[0].StretchOrder)
	}
}

// §863 gives it to the FINAL pass. A caller running \pretolerance or \tolerance
// leaves FinalPass false, and then the knob must do nothing at all — otherwise
// the first pass silently gets the emergency glue and the three-pass structure
// collapses into one.
func TestEmergencyStretchIsConfinedToTheFinalPass(t *testing.T) {
	base := DefaultParams(200, 10)
	base.FinalPass = false

	plain, okA := KnuthPlassWith(emergencyItems(), emergencyMeasure, base)

	loaded := base
	loaded.EmergencyStretch = 15
	armed, okB := KnuthPlassWith(emergencyItems(), emergencyMeasure, loaded)

	if okA != okB {
		t.Fatalf("ok differs with FinalPass false: %v vs %v — the knob escaped its pass", okA, okB)
	}
	if len(plain) != len(armed) {
		t.Fatalf("line count differs with FinalPass false: %d vs %d", len(plain), len(armed))
	}
	for i := range plain {
		if plain[i] != armed[i] {
			t.Errorf("line %d differs with FinalPass false:\n without = %+v\n with    = %+v\n"+
				"\\emergencystretch must be inert outside the final pass (tex.web §863)", i, plain[i], armed[i])
		}
	}
}

// ⭐ The point of the finding: raising the tolerance and adding emergency
// stretch both make the paragraph "work", and they are not the same thing.
// This is the assertion that would have caught the substitute the consumer
// used — a maximal tolerance accepts the line AS IT IS, at ratio 16; emergency
// stretch makes the line fit, at ratio 1. Badness is cubic in that ratio, so
// the two charge demerits four orders of magnitude apart and choose differently
// wherever there is a choice.
func TestARaisedToleranceIsNotEmergencyStretch(t *testing.T) {
	tol := DefaultParams(MaxBadRatio, 10)
	tol.FinalPass = true
	byTolerance, ok := KnuthPlassWith(emergencyItems(), emergencyMeasure, tol)
	if !ok || len(byTolerance) != 1 {
		t.Fatalf("tolerance arm: ok=%v lines=%d", ok, len(byTolerance))
	}

	es := DefaultParams(200, 10)
	es.FinalPass = true
	es.EmergencyStretch = 15
	byStretch, ok := KnuthPlassWith(emergencyItems(), emergencyMeasure, es)
	if !ok || len(byStretch) != 1 {
		t.Fatalf("stretch arm: ok=%v lines=%d", ok, len(byStretch))
	}

	if math.Abs(byTolerance[0].Ratio-byStretch[0].Ratio) < 1 {
		t.Errorf("the two paths agree (ratios %v and %v): this test exists because they must not — "+
			"a maximal tolerance accepts a bad line, \\emergencystretch makes a good one",
			byTolerance[0].Ratio, byStretch[0].Ratio)
	}
	if got := byTolerance[0].Ratio; math.Abs(got-16) > 1e-9 {
		t.Errorf("tolerance arm ratio = %v, want 16: raising the tolerance does not add glue", got)
	}
}
