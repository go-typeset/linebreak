package linebreak

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// ⚠ THE WITNESS FOR THIS FORMAT IS tex.web's PRINTING CODE, NOT A CAPTURED LOG,
// and that limit is stated rather than left for a reader to discover.
//
// Every field below is pinned against the statements that emit it — :16858 for
// the "@", :16860-16866 for the kind, :16867-16869 for "via @@<serial>", :16871
// for b and its star, :16873 for p, :16874 for d and its star, and :16665-16673
// for the whole active-node line. What has NOT been done is diffing this against
// a real TeX log, because no TeX that emits one is installed here: every
// tracing_paragraphs site in tex.web sits inside the `@!stat … tats`
// conditional, and the tectonic on this host prints \tracingcommands and
// nothing for \tracingparagraphs — measured, with that control, on
// go-tex/engine#768. A web2c tex or pdftex on the measurement host would close
// that gap, and until then this test pins an INTENT read off the source.
func TestTheTraceIsTeXsOwnFormat(t *testing.T) {
	// All four kinds TeX prints that this package can reach: a break at glue
	// (which prints no kind at all), a flagged penalty (\discretionary), an
	// UNFLAGGED one (\penalty), and the paragraph's end (\par).
	items := []Item{
		Box(4),                         // 0
		Glue(1, 4, 1),                  // 1  a glue break
		Box(4),                         // 2
		Penalty(0, 50, true),           // 3  a \discretionary break
		Box(4),                         // 4
		Penalty(0, 100, false),         // 5  a \penalty break
		Box(4),                         // 6
		Glue(1, 4, 1),                  // 7
		Box(4),                         // 8
		GlueOrder(0, 1, OrderFil, 0),   // 9  \parfillskip
		Penalty(0, -InfPenalty, false), // 10 the forced end
	}
	var log bytes.Buffer
	p := DefaultParams(10000, 10)
	p.Trace = &log
	if _, ok := KnuthPlassWith(items, 10, p); !ok {
		t.Fatal("no solution")
	}
	got := log.String()
	t.Logf("trace:\n%s", got)

	// The two line shapes, by prefix rather than by whole-line equality: the
	// numbers depend on this fixture's arithmetic, the SHAPE is TeX's.
	for _, want := range []string{
		"@ via @@0 b=",           // a break at glue prints NO kind (:16861)
		`@\discretionary via @@`, // :16863
		`@\penalty via @@`,       // :16862
		`@\par via @@`,           // cur_p = null (:16860)
		"@@1: line 1.",           // the first active node, serial 1 (:16665-16667)
		" -> @@0",                // its ancestor is the paragraph's start (:16672)
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the trace has no %q:\n%s", want, got)
		}
	}
	// Every "via @@N" must name a serial the log has already DEFINED, or the
	// chain a reader follows backwards is broken.
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if i := strings.Index(line, "via @@"); i >= 0 {
			ref := line[i+len("via @@"):]
			ref = strings.Fields(ref)[0]
			if ref == "0" {
				continue
			}
			if !strings.Contains(got, "@@"+ref+": line ") {
				t.Errorf("%q cites @@%s, which no line defines", line, ref)
			}
		}
	}
}

// ⛔ A TRACE THAT CHANGES THE ANSWER IS NOT A TRACE. The breaking must be
// identical with the log on and off — otherwise the instrument is part of what
// it measures, and every divergence it shows is suspect.
func TestTheTraceChangesNothing(t *testing.T) {
	items := []Item{
		Box(4), Glue(1, 4, 1), Box(4), Penalty(0, 50, true), Box(4),
		Glue(1, 4, 2), Box(5), Penalty(0, 50, true), Box(2),
		Glue(1, 4, 1), Box(4),
		GlueOrder(0, 1, OrderFil, 0), Penalty(0, -InfPenalty, false),
	}
	quiet := DefaultParams(200, 10)
	silent, ok1 := KnuthPlassWith(items, 10, quiet)

	loud := DefaultParams(200, 10)
	var log bytes.Buffer
	loud.Trace = &log
	traced, ok2 := KnuthPlassWith(items, 10, loud)

	if ok1 != ok2 {
		t.Fatalf("feasibility differs: %v without the trace, %v with it", ok1, ok2)
	}
	if !reflect.DeepEqual(silent, traced) {
		t.Errorf("the breaking differs with the trace on:\n without %+v\n with    %+v", silent, traced)
	}
	if log.Len() == 0 {
		t.Error("nothing was traced, so the comparison above proves nothing")
	}
}

// The star is the whole value of the d field: it says a break was taken because
// the paragraph had no other way out. tex.web:16874.
func TestArtificialDemeritsPrintAStar(t *testing.T) {
	// One unbreakable box far wider than the measure: no feasible break exists,
	// so the final pass must take an artificial one.
	items := []Item{
		Box(80),
		GlueOrder(0, 1, OrderFil, 0),
		Penalty(0, -InfPenalty, false),
	}
	p := DefaultParams(200, 10)
	p.FinalPass = true
	var log bytes.Buffer
	p.Trace = &log
	if _, ok := KnuthPlassWith(items, 10, p); !ok {
		t.Fatal("the final pass found no solution at all")
	}
	if !strings.Contains(log.String(), "d=*") {
		t.Errorf("no artificial break was marked d=*:\n%s", log.String())
	}
}

// ⛔ b=* IS NOT "BADNESS ≥ 10000", and the first version of this test assumed it
// was and failed: §108 caps badness AT inf_bad, so every merely-dreadful line
// prints b=10000 and a `> 10000` test is dead code. TeX reaches inf_bad+1 by
// assignment in ONE case — a line that "cannot shrink enough to fit the
// necessary width" (:16753-16756, :16810) — which is r < −1 here.
//
// So the witness is an OVERFULL line, not a stretched one. The final pass must
// be on, because a line that cannot shrink is never feasible and only
// artificial_demerits records it at all: the two stars appear together, which is
// itself what TeX's log looks like at the end of a hopeless paragraph.
func TestABadnessPastInfBadPrintsAStar(t *testing.T) {
	items := []Item{
		Box(80), // far wider than the measure, and nothing can shrink it
		GlueOrder(0, 1, OrderFil, 0),
		Penalty(0, -InfPenalty, false),
	}
	p := DefaultParams(200, 10)
	p.FinalPass = true
	var log bytes.Buffer
	p.Trace = &log
	if _, ok := KnuthPlassWith(items, 10, p); !ok {
		t.Fatal("the final pass found no solution at all")
	}
	if !strings.Contains(log.String(), "b=*") {
		t.Errorf("an overfull line was not marked b=*:\n%s", log.String())
	}
	// And the control, so this cannot pass by starring everything: a line that
	// merely stretches badly prints a NUMBER, capped at inf_bad.
	var ok2 bytes.Buffer
	q := DefaultParams(100000, 10)
	q.Trace = &ok2
	if _, ok := KnuthPlassWith([]Item{
		Box(1), Glue(1, 1, 1), Box(1),
		GlueOrder(0, 1, OrderFil, 0), Penalty(0, -InfPenalty, false),
	}, 400, q); !ok {
		t.Fatal("no solution for the control")
	}
	if !strings.Contains(ok2.String(), "b=10000") {
		t.Errorf("a very bad but shrinkable line should print the capped badness:\n%s", ok2.String())
	}
	if strings.Contains(ok2.String(), "b=*") {
		t.Errorf("a line that is merely bad was starred:\n%s", ok2.String())
	}
}
