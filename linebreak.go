// Copyright (c) the go-typeset/linebreak authors.
// SPDX-License-Identifier: BSD-3-Clause

package linebreak

import (
	"fmt"
	"io"
	"math"
)

// Package linebreak chooses where to break a paragraph into lines so that the
// whole paragraph reads as evenly as possible. Given a horizontal list of items
// (boxes, glue and penalties) and a line width, it returns the breakpoints that
// minimise the paragraph's total cost — the Knuth–Plass algorithm (1981).

// ItemKind classifies a horizontal-list item.
type ItemKind uint8

const (
	KBox     ItemKind = iota // a box of fixed width
	KGlue                    // stretchable/shrinkable space (a legal breakpoint after a box)
	KPenalty                 // a penalty (a legal breakpoint; ±InfPenalty = forbidden/forced)
)

// Item is one element of a horizontal list.
type Item struct {
	Kind            ItemKind
	Width           float64
	Height, Depth   float64 // box only (glyph metrics)
	R               rune    // box only (the glyph, 0 if none)
	Stretch, Shrink float64 // glue only
	StretchOrder    uint8   // glue only: 0 = pt, 1 = fil, 2 = fill, 3 = filll
	Penalty         float64 // penalty only
	Flagged         bool    // penalty only (e.g. a hyphen) — consecutive flags are penalised
}

// The glue orders TeX keeps apart (tex.web §16781-16782: "the available
// stretchability can be found in the subarray cur_active_width[2..5], in units
// of points, fil, fill, and filll").
//
// ⛔ THEY ARE NOT MAGNITUDES, THEY ARE CLASSES. A `plus 1fil` is not "a very
// large number of points": any amount of fil beats every amount of points, and
// a line carrying any of them has badness 0 (§16790-16793). A consumer that
// flattens fil to 1e6 points gets a large but FINITE badness, so a line TeX
// calls perfect comes out merely tolerable — and under \raggedright the whole
// first pass then fails and the hyphens appear (go-typeset/linebreak#12 item 1,
// seen from go-tex/engine).
//
// ⚠ SHRINK HAS NO ORDERS HERE, and that is tex.web's own word: §16806,
// "Shrinkability is never infinite in a paragraph". A \hskip 0pt minus 1fil
// does exist, but line_break never sees one in the background and the shrink
// column stays a single total.
const (
	OrderPt    uint8 = 0 // finite, in whatever unit the caller uses
	OrderFil   uint8 = 1
	OrderFill  uint8 = 2
	OrderFilll uint8 = 3

	numOrders = 4
)

// Box, Glue and Penalty are constructors.
func Box(w float64) Item { return Item{Kind: KBox, Width: w} }

// Glyph is a box carrying a rune and its height/depth (used by the typesetter).
func Glyph(r rune, w, h, d float64) Item {
	return Item{Kind: KBox, Width: w, Height: h, Depth: d, R: r}
}
func Glue(w, stretch, shrink float64) Item {
	return Item{Kind: KGlue, Width: w, Stretch: stretch, Shrink: shrink}
}

// GlueOrder is Glue whose stretch is of the given order: GlueOrder(0, 1,
// OrderFil, 0) is TeX's `\hskip 0pt plus 1fil`. Order OrderPt is exactly Glue.
func GlueOrder(w, stretch float64, order uint8, shrink float64) Item {
	if order >= numOrders {
		order = numOrders - 1
	}
	return Item{Kind: KGlue, Width: w, Stretch: stretch, StretchOrder: order, Shrink: shrink}
}
func Penalty(w, p float64, flagged bool) Item {
	return Item{Kind: KPenalty, Width: w, Penalty: p, Flagged: flagged}
}

// InfPenalty is the "infinite" penalty: +InfPenalty forbids a break, −InfPenalty
// forces one (the end of a paragraph is a forced break).
const InfPenalty = 10000.0

// Line describes one output line of a broken paragraph.
type Line struct {
	Start, End int     // item index range [Start, End) actually set on the line
	Ratio      float64 // glue adjustment ratio r (−1 fully shrunk … +tolerance stretched)

	// Indent is the line's left margin, from the Measure the paragraph was
	// broken with: 0 for every line of a uniform paragraph, and the hanging or
	// \parshape margin otherwise.
	//
	// ⚠ IT IS NOT DERIVABLE FROM THE WIDTH. A negative \hangindent shortens
	// the line and leaves the margin at ZERO — it indents from the right — so a
	// consumer computing "hsize minus the width" would place those lines wrong
	// (tex.web:16723-16724, :16729-16730). It is reported here so the rule
	// lives once, in HangIndent, rather than once per caller.
	Indent float64

	// StretchOrder is the order Ratio is measured against: the HIGHEST order
	// present on the line, since that one absorbs all of the slack and the
	// lower ones are set at their natural width (tex.web §16620-16632, the same
	// rule hpack uses).
	//
	// ⛔ A CONSUMER MUST READ IT TO SET THE LINE. `Ratio = 0.3` means "stretch
	// every glue by 0.3 of its stretch" only when StretchOrder is OrderPt. At
	// OrderFil it means "stretch the FIL glues by 0.3 of their fil and leave
	// every finite glue alone" — distributing it over all the glues instead
	// would spread a \raggedright line's slack through its interword spaces,
	// which is precisely what \raggedright exists to prevent.
	StretchOrder uint8
}

// breakNode is an active breakpoint in the dynamic program.
type breakNode struct {
	position int        // item index of this breakpoint
	line     int        // number of the line ending here
	fitness  int        // fitness class of the line ending here (veryLoose…tight)
	demerits float64    // total demerits of the best path to here
	prev     *breakNode // the breakpoint that begins this line
	serial   int        // tex.web's serial(passive), for Trace; 0 is the paragraph's start
}

// The four fitness classes TeX sorts lines into, by how hard their spaces had to
// work (tex.web §16099-16105). Two adjacent lines whose classes differ by more
// than one look wrong side by side — a very loose line under a tight one — and
// cost AdjDemerits.
const (
	veryLoose = 0 // stretching more than its stretchability
	loose     = 1 // stretching 0.5 to 1.0 of it
	decent    = 2 // everything else
	tight     = 3 // shrinking 0.5 to 1.0 of its shrinkability
)

// Params carries the costs TeX charges besides the line's own badness. The zero
// value is not useful; DefaultParams holds the values every LaTeX document runs
// with (latex.ltx:498-514).
type Params struct {
	// ScaledPointsPerUnit asks for tex.web §108's integer badness (TeXBadness)
	// instead of the float 100·r³, and says how many scaled points one unit of
	// every width, stretch and shrink handed in is: 1 when they are already
	// scaled points, 65536 when they are TeX points. Each is multiplied by it
	// and rounded to an integer sp before §108 sees it, which is exact for any
	// value that is a whole number of sp (a power-of-two division like
	// sp/65536 round-trips). Zero, the default, is the unit-free float that
	// non-TeX consumers have always had.
	ScaledPointsPerUnit float64
	Tolerance           float64 // \tolerance: the worst BADNESS a line may have (100·ratio³)
	LinePenalty         float64 // \linepenalty: charged once per line, so fewer lines cost less
	AdjDemerits         float64 // \adjdemerits: adjacent lines two fitness classes apart
	DoubleHyph          float64 // \doublehyphendemerits: two hyphenated lines in a row
	FinalHyph           float64 // \finalhyphendemerits: a hyphen on the last line but one

	// Trace receives TeX's own \tracingparagraphs log, or nothing when nil.
	//
	// ⛔ THE FORMAT IS tex.web's, TO THE CHARACTER, BECAUSE ITS WHOLE VALUE IS
	// BEING DIFFABLE AGAINST THE REFERENCE. A divergence in line breaking is
	// otherwise inferred from glyph positions three layers downstream; with this
	// the two engines state their own reasons — the badness, the penalty and the
	// demerits of every break each one considered — and the first line that
	// differs is the divergence.
	//
	// Two line kinds, and a caller writes the pass markers (@firstpass,
	// @secondpass, @emergencypass) around its own passes, since it is the caller
	// that runs them:
	//
	//	@\discretionary via @@0 b=131 p=50 d=11781   a feasible break (:16854-16875)
	//	@@1: line 1.2- t=11781 -> @@0                a new active node (:16664-16674)
	//
	// `*` stands for "past inf_bad" in b and for artificial_demerits in d, as in
	// TeX. The fitness digit is TeX's own numbering, which this package already
	// shares: 0 very loose, 1 loose, 2 decent, 3 tight (:16099-16105). The `-`
	// after it marks a hyphenated break.
	//
	// ⚠ What it does NOT reproduce is §16877's short_display of the material
	// between breaks: this package is handed items and never the text, so the
	// words TeX prints above each break line have no counterpart here. A diff
	// against a real TeX log has to ignore those, which is why they are left out
	// rather than approximated.
	Trace io.Writer

	// FinalPass turns on TeX's last resort: artificial_demerits.
	//
	// ⛔⛔ WITHOUT IT AN OVERFULL SINGLE LINE BEATS TWO INFEASIBLE SHORT ONES, and TeX
	// chooses the other way. tex.web §16769-16771: the deactivation at an impossible
	// line is guarded by "and |goto deactivate| UNLESS there is a reason to consider
	// lines of text from |r| to |cur_p|", and the reason is
	//
	//	if final_pass and (minimum_demerits=awful_bad) and
	//	   (link(r)=last_active) and (prev_r=active) then
	//	  artificial_demerits:=true {set demerits zero, this break is forced}
	//	else if b>threshold then goto deactivate;
	//
	// — when nothing feasible has been recorded at this breakpoint and r is the ONLY
	// active node, the break is recorded at ZERO cost. Zero is what makes it win:
	// badness past inf_bad costs 1e8 per line, so two hopeless lines (2e8) otherwise
	// lose to one overfull line (1e8).
	//
	// Measured in go-tex/engine: two unbreakable 35pt words with a 3pt rigid space on
	// a 40pt measure. Neither line can be set — each is 5pt short with nothing to
	// stretch — and the reference puts them on TWO lines. Without this flag the
	// breaker returned ONE line 33pt over the margin, because it cost half as much.
	//
	// It is the caller's business because the PASSES are: \pretolerance, then
	// \tolerance, then the emergency pass. Only the last one may fall back, and only
	// the caller knows which pass it is running.
	FinalPass bool

	// EmergencyStretch is \emergencystretch: extra FINITE stretch given to every
	// line, and only on the final pass (tex.web §863 adds it to background[2],
	// the finite slot, when that pass is entered).
	//
	// ⛔⛔ IT IS NOT A LOOSER TOLERANCE, AND SUBSTITUTING ONE IS A DIFFERENT
	// ALGORITHM. A consumer without this knob raises \tolerance instead — go-tex's
	// paragraph.go:251-258 sets it to its maximum — which ACCEPTS arbitrarily bad
	// lines at the original glue. This makes the lines GOOD instead: the same
	// paragraph that was infeasible at ratio 5 (badness 12500) is set at ratio 1
	// (badness 100) once the stretch is actually there. Both "succeed"; they
	// choose different breaks and charge different demerits, because badness is
	// cubic in the ratio and the ratio is what changed.
	//
	// ⚠ FINITE, order 0, deliberately. Infinite stretch would make every line
	// badness 0 (§16790-16793) and the emergency pass would stop discriminating
	// between breaks at all — which is \raggedright, not an emergency pass.
	//
	// It is added once per line rather than as an item because that is what a
	// background is: §16307-16315 seeds active_width with it before any item is
	// walked. A caller running the first two passes leaves this zero.
	EmergencyStretch float64

	// Looseness is \looseness: how many lines longer (or, negative, shorter) the
	// paragraph should come out than the breaking would otherwise make it
	// (tex.web:17197-17237).
	//
	// ⛔⛔ IT DOES NOT RE-BREAK THE PARAGRAPH. Every feasible way of ending the
	// paragraph is already an active node when the last break is tried; TeX picks
	// the one with fewest demerits, calls its line count best_line, and then — only
	// if \looseness is non-zero — walks the SAME nodes again looking for one whose
	// line count differs from best_line in the direction asked for:
	//
	//	line_diff:=line_number(r)-best_line;
	//	if ((line_diff<actual_looseness)and(looseness<=line_diff))or
	//	   ((line_diff>actual_looseness)and(looseness>=line_diff)) then
	//	  begin best_bet:=r; actual_looseness:=line_diff; … end
	//	else if (line_diff=actual_looseness)and(total_demerits(r)<fewest_demerits)…
	//
	// So it is a CHOICE among breakings that already exist, not a second search,
	// and a paragraph with only one feasible ending cannot be loosened at all.
	//
	// ⛔ AND WHEN THE LOOSENESS CANNOT BE MATCHED, THE PASS FAILS. tex.web:17203 is
	// `if (actual_looseness=looseness)or final_pass then goto done` — otherwise the
	// pass ends without a result and the next, looser one runs, because a looser
	// threshold admits more active nodes and therefore more line counts to choose
	// between. That is reported here the way this package reports any failed pass:
	// ok=false, with FinalPass accepting whatever was achieved. A caller that runs
	// only one pass and sets Looseness must set FinalPass with it, or a paragraph
	// it cannot loosen comes back as no paragraph.
	Looseness int

	// Background is what every line carries before its own material: TeX's
	// \leftskip and \rightskip, which are added to EVERY line rather than
	// appearing in the item list (tex.web §16307-16315, verbatim):
	//
	//	q:=left_skip; r:=right_skip; background[1]:=width(q)+width(r);
	//	background[2+stretch_order(q)]:=stretch(q);
	//	background[2+stretch_order(r)]:=background[2+stretch_order(r)]+stretch(r);
	//	background[6]:=shrink(q)+shrink(r);
	//
	// ⛔ THIS IS WHAT MAKES \raggedright WORK, and it has to be here rather
	// than in the caller. \raggedright is \rightskip = 0pt plus 1fil: every
	// line then carries fil stretch, every line has badness 0 (§16790-16793),
	// and TeX's FIRST pass — no hyphenation, at \pretolerance — succeeds
	// outright. A consumer that breaks the paragraph justified and adds the
	// skips afterwards gets the hyphens of a justified paragraph with ragged
	// glue painted on top (go-tex/engine#583 and its paragraph.go:215-241,
	// which had to guess at this by opening the tolerance instead).
	Background Background
}

// Background is the glue every line carries before its own material: in TeX,
// \leftskip + \rightskip. The zero value adds nothing, which is a justified
// paragraph with both skips at 0pt.
type Background struct {
	Width   float64            // widths, summed
	Stretch [numOrders]float64 // stretch by order, as §16307-16315 sums them
	Shrink  float64            // shrink, summed (never infinite: §16806)
}

// DefaultParams are LaTeX's own settings (latex.ltx:498-514): \tolerance=200,
// \linepenalty=10, \adjdemerits=10000, \doublehyphendemerits=10000,
// \finalhyphendemerits=5000.
func DefaultParams(tolerance, linePenalty float64) Params {
	return Params{
		Tolerance: tolerance, LinePenalty: linePenalty,
		AdjDemerits: 10000, DoubleHyph: 10000, FinalHyph: 5000,
	}
}

// KnuthPlass breaks items into lines of the given width, minimising total
// cost (linePenalty is charged once per line, which discourages breaking a
// paragraph into many short ones). It returns the chosen lines in
// order and ok=false if no sequence of feasible breaks exists within tolerance.
func KnuthPlass(items []Item, lineWidth, tolerance, linePenalty float64) ([]Line, bool) {
	return KnuthPlassWith(items, lineWidth, DefaultParams(tolerance, linePenalty))
}

// KnuthPlassWith is KnuthPlass with the costs stated explicitly, for a caller
// that threads a document's own \adjdemerits and friends.
//
// The measure is one width for the whole paragraph, which is right for the
// overwhelmingly common case. A paragraph with \hangindent or \parshape needs a
// width PER LINE: see KnuthPlassShaped.
func KnuthPlassWith(items []Item, lineWidth float64, p Params) ([]Line, bool) {
	return KnuthPlassShaped(items, Uniform(lineWidth), p)
}

// KnuthPlassShaped breaks a paragraph whose lines do not all have the same
// width: \hangindent with \hangafter, or \parshape.
//
// ⛔⛔ THE WIDTH IS AN INPUT TO EVERY CANDIDATE BREAK'S BADNESS, which is why
// this cannot be done by breaking on one measure and indenting afterwards.
// tex.web §847-850 recomputes line_width for each line inside the breaking
// loop. Measured by a consumer against tectonic on a \hangindent=4em paragraph:
// not only was the indent missing, the paragraph BROKE DIFFERENTLY — four ems
// more text per line moved a phrase from the reference's line 4 onto line 3,
// and a line gained or lost at the end of a paragraph becomes a page gained or
// lost (go-typeset/linebreak#11).
//
// The line numbers handed to the Measure are 1-based, as TeX's are.
// lineSlot holds the best break of each fitness class for ONE line number —
// tex.web's (line, fitness) half of its (position, line, fitness) state
// (:16107-16114). With \looseness zero every line number shares slot 0, which is
// the collapse :16716 licenses and the behaviour this package always had.
type lineSlot struct {
	line int
	best [4]*breakNode
}

// chooseLooseness is tex.web's "Find the best active node for the desired
// looseness" (:17221-17237), over the nodes that end the paragraph. It returns
// the node to break at and whether the looseness asked for was reached.
//
// best is the fewest-demerits node and its line count is TeX's best_line; a
// candidate is taken when its line_diff moves actual_looseness TOWARDS the
// target without overshooting it, and ties on line_diff are settled by demerits.
// Reading the two conditions as one sentence: among the endings on the side of
// best_line that \looseness asks for, take the one furthest from best_line that
// does not pass the target.
func chooseLooseness(finals []*breakNode, best *breakNode, looseness int) (*breakNode, bool) {
	bestLine := best.line
	chosen, actual, fewest := best, 0, best.demerits
	for _, r := range finals {
		diff := r.line - bestLine
		switch {
		case (diff < actual && looseness <= diff) || (diff > actual && looseness >= diff):
			chosen, actual, fewest = r, diff, r.demerits
		case diff == actual && r.demerits < fewest:
			chosen, fewest = r, r.demerits
		}
	}
	return chosen, actual == looseness
}

func KnuthPlassShaped(items []Item, m Measure, p Params) ([]Line, bool) {
	// Prefix sums of width/stretch/shrink over items strictly before index i, so
	// the material of a line from break a to break b is sums[b]−sums[a].
	// ⛔ ONE COLUMN OF STRETCH PER ORDER. There was one, and a fil had to be
	// flattened into it by the caller, which is #12 item (1).
	n := len(items)
	sumW := make([]float64, n+1)
	var sumY [numOrders][]float64
	for o := range sumY {
		sumY[o] = make([]float64, n+1)
	}
	sumZ := make([]float64, n+1)
	for i, it := range items {
		sumW[i+1] = sumW[i]
		// ⛔⛔ A PENALTY'S WIDTH IS NOT PART OF THE RUNNING TOTAL. It is tex.web's
		// disc_width, and :17128-17137 adds it for ONE try_break and takes it back:
		//
		//	act_width:=act_width+disc_width;
		//	try_break(hyphen_penalty,hyphenated);
		//	act_width:=act_width-disc_width;
		//
		// so a discretionary's pre-break material — the hyphen — is charged to the
		// line that BREAKS there and to no other. measure() below adds it at the
		// break, which was already right; putting it in the prefix sums as well
		// charged it to every line that merely SPANS the hyphenation point.
		//
		// That is go-typeset/linebreak#12 item (2), and the consumer had written the
		// consequence into its own source rather than hide it: go-tex/engine passes
		// Penalty(0, …) for every discretionary, with a comment explaining that a
		// real width "would inflate every line holding an unbroken hyphenation
		// point". The cost of that workaround is the other half: the line that DOES
		// break at a hyphen is then measured without the hyphen it will carry —
		// about 2.5pt at 10pt, on a 45pt measure.
		if it.Kind != KPenalty {
			sumW[i+1] += it.Width
		}
		for o := range sumY {
			sumY[o][i+1] = sumY[o][i]
		}
		sumZ[i+1] = sumZ[i]
		if it.Kind == KGlue {
			o := it.StretchOrder
			if o >= numOrders {
				o = numOrders - 1
			}
			sumY[o][i+1] += it.Stretch
			sumZ[i+1] += it.Shrink
		}
	}

	// legal reports whether a break may occur at item b (glue after a box, or a
	// non-forbidden penalty, or the very end).
	legal := func(b int) bool {
		if b == n {
			return true
		}
		switch items[b].Kind {
		case KGlue:
			return b > 0 && items[b-1].Kind == KBox
		case KPenalty:
			return items[b].Penalty < InfPenalty
		}
		return false
	}

	// width/stretch/shrink of the line from break a to break b. Leading glue just
	// after break a is discarded; a penalty break contributes its own width.
	//
	// ⛔⛔ a > 0, BECAUSE POSITION 0 IS NOT A BREAK. The active list is seeded with
	// {position: 0}, the paragraph's beginning, and there is no "glue we broke at"
	// there — tex.web:17001 says so in one line, `prev_p:=cur_p; {glue at beginning is
	// not a legal breakpoint}`, and legal() above already refuses it. Discarding it
	// anyway threw away a paragraph's opening glue: \noindent\hskip 20pt lost its
	// 20pt, the reference starting that line at x=153.69 and go-tex at 133.77
	// (go-tex/engine#657).
	//
	// ⛔⛔ AND IT IS THE WHOLE RUN, NOT ONE GLUE — see lineStart, which both this and
	// the reconstruction below now call, so the Ratio is measured on the span the
	// Start is reported for.
	// ⚠ AND THE BACKGROUND IS ADDED HERE, once per line, because that is what
	// it is: §16307-16315 seeds active_width with it before any item is walked,
	// so every line's totals start from it rather than from zero.
	// §863: entering the emergency pass adds \emergencystretch to background[2],
	// the FINITE stretch slot — so it is folded in here, once, rather than tested
	// per line. A caller not on the final pass leaves EmergencyStretch zero and
	// this is p.Background unchanged.
	bg := p.Background
	if p.FinalPass {
		bg.Stretch[0] += p.EmergencyStretch
	}
	measure := func(a, b int) (w float64, y [numOrders]float64, z float64) {
		start := lineStart(items, a, b)
		w = sumW[b] - sumW[start] + bg.Width
		for o := range y {
			y[o] = sumY[o][b] - sumY[o][start] + bg.Stretch[o]
		}
		z = sumZ[b] - sumZ[start] + bg.Shrink
		if b < n && items[b].Kind == KPenalty {
			w += items[b].Width
		}
		return
	}

	// The paragraph starts as if the line before it had been decent (tex.web
	// §17032, fitness(q):=decent_fit), so a first line two classes away from decent
	// is charged like any other mismatch.
	active := []*breakNode{{position: 0, line: 0, fitness: decent}}
	var best *breakNode
	// Every node that ends the paragraph, kept for \looseness (see chooseLooseness).
	var finals []*breakNode
	// serial numbers the passive nodes for Trace, as tex.web does: the start of
	// the paragraph is @@0 and the first real break is @@1.
	serial := 0

	// paraEnd is where the paragraph's LAST line ends: the last forced penalty in the
	// list, or n when there is none.
	//
	// ⛔ A paragraph is terminated by \parfillskip and a forced penalty (tex.web
	// §16077-16084), so that penalty is the end. n is one PAST the final item, and a
	// break there yields a line holding nothing but the terminator.
	paraEnd := n
	for i := n - 1; i >= 0; i-- {
		if items[i].Kind == KPenalty && items[i].Penalty <= -InfPenalty {
			paraEnd = i
			break
		}
	}

	for b := 1; b <= n; b++ {
		if !legal(b) {
			continue
		}
		forced := b == n || (items[b].Kind == KPenalty && items[b].Penalty <= -InfPenalty)
		var survivors []*breakNode
		// TeX keeps the best break for EACH fitness class, not one overall
		// (§16480-16486): a line that is merely decent may be the wrong ancestor for
		// the next one, and only keeping all four lets \adjdemerits decide.
		//
		// ⛔⛔ AND BY LINE NUMBER TOO WHEN \looseness IS NON-ZERO. tex.web:16107-16114
		// says what the state is — "the best possible way to achieve each feasible
		// combination of position, line, and fitness" — and then that collapsing the
		// line dimension is an OPTIMISATION, licensed only "when the looseness
		// parameter is zero". :16716 is the switch, in one line:
		//
		//	if looseness=0 then easy_line:=last_special_line
		//	else easy_line:=max_halfword
		//
		// With easy_line at max_halfword no line number is ever equivalent to
		// another, so endings of different LENGTHS survive side by side — which is
		// the list \looseness then chooses from. Keyed by fitness alone, a paragraph
		// offered exactly one ending and chooseLooseness had nothing to pick between:
		// the parameter was implemented and inert.
		var slots []lineSlot
		slotAt := func(line int) int {
			key := 0 // looseness 0: every line number is the same slot, as before
			if p.Looseness != 0 {
				key = line
			}
			for i := range slots {
				if slots[i].line == key {
					return i
				}
			}
			slots = append(slots, lineSlot{line: key})
			return len(slots) - 1
		}
		// ⛔ A SLOT EXISTS ONLY BECAUSE A NODE WENT INTO IT: slotAt is called one
		// line before the assignment below, and a brand-new slot's class is nil, so
		// that assignment always happens. "has anything been recorded at this break
		// position" is therefore len(slots) > 0, and the four-way nil test this
		// replaced (bestHere[0]==nil && … ) had the same meaning when there was one
		// implicit slot.
		recorded := func() bool { return len(slots) > 0 }
		// onlyActive mirrors TeX's "link(r)=last_active and prev_r=active": r is the
		// one and only active node, so deactivating it loses the paragraph.
		onlyActive := len(active) == 1
		for _, a := range active {
			w, y, z := measure(a.position, b)
			// The line being tried runs FROM a, so it is a's line plus one —
			// the same number the node recorded for it below (line: a.line+1).
			// tex.web's "Compute the new line width" (:16740-16748) looks it up
			// per candidate exactly here, inside try_break.
			_, lineWidth := m.LineShape(a.line + 1)
			r, order := ratio(w, y, z, lineWidth)
			// A node stays active for *later* breaks only while the line to here is
			// not already overfull past its shrink (r ≥ −1).
			//
			// ⛔⛔ AND NEVER ACROSS A FORCED BREAK. tex.web §16769:
			//
			//	if (b>inf_bad)or(pi=eject_penalty) then
			//	  @<Prepare to deactivate node~|r| …@>
			//
			// Every active node is deactivated at an eject_penalty, so no path reaches
			// past it: the break is MANDATORY, not merely tolerated however badly the
			// line fits. Keeping the survivors let a solution span the forced penalty
			// and win on demerits at the end of the paragraph, where b == n is forced
			// too — so \\ and \linebreak simply did not break.
			//
			// Measured in go-tex/engine, which splits a paragraph at forced penalties to
			// work around this: with the split removed and this line as it was,
			// `un deux\penalty-10000 trois quatre` came out on ONE line and the \hfil of
			// a \\ stretched in the middle of it (onze at x=298.97 beside neuf dix).
			if r >= -1 && !forced {
				survivors = append(survivors, a)
			}
			// A break is taken from a if the line is feasible, or if the break is
			// forced (then even an over/underfull line must close the paragraph).
			// Feasible means BADNESS within tolerance, not the ratio (tex.web
			// §16773, "if b>threshold then goto continue"): badness grows as the
			// cube of the ratio, so \tolerance=200 admits a line stretched by about
			// 1.26 of its stretchability and nothing looser.
			// feasible is tex.web's "b<=threshold": the line's BADNESS is within
			// tolerance. A line overfull past its shrink has b=awful_bad, which no
			// threshold admits, and that is the r < -1 half.
			//
			// ⛔⛔ AND A LINE CARRYING ANY INFINITE STRETCH HAS b = 0, so it is
			// feasible at EVERY tolerance, however short it is (§16790-16793). That
			// is the whole of \raggedright: with \rightskip = 0pt plus 1fil in the
			// background, every line is perfect and TeX's first pass — which has no
			// hyphens at all — succeeds. Before this, a fil had to be flattened to a
			// large finite stretch by the caller, the badness came out large but
			// finite, the first pass failed, and the hyphens of a JUSTIFIED paragraph
			// appeared under ragged glue (#12 item 1).
			lb := p.lineBad(w, y, z, lineWidth, r, order)
			feasible := r >= -1 && lb <= p.Tolerance
			// ⛔⛔ TEX'S LAST RESORT IS INSIDE ONE BRANCH, AND THAT BRANCH IS THE WHOLE
			// RULE. tex.web §16769-16775, the three sentences in their own order:
			//
			//	if (b>inf_bad)or(pi=eject_penalty) then
			//	  begin if final_pass and (minimum_demerits=awful_bad) and
			//	     (link(r)=last_active) and (prev_r=active) then
			//	    artificial_demerits:=true {set demerits zero, this break is forced}
			//	  else if b>threshold then goto deactivate;
			//	  node_r_stays_active:=false;
			//	  end
			//	else begin prev_r:=r;
			//	  if b>threshold then goto continue;
			//	  node_r_stays_active:=true;
			//	  end;
			//
			// Two things follow, and this code had neither.
			//
			// ⛔ (a) A FORCED BREAK PAST THE THRESHOLD IS NOT RECORDED — `goto deactivate`
			// skips the recording entirely. The condition read `… || forced || …`, so
			// every forced break was recorded however badly its line fitted
			// (go-typeset/linebreak#12 item 5). A pass that cannot set the paragraph
			// within \tolerance must FAIL and let the caller run the next one; recording
			// the bad break made it succeed with a bad line instead, which is why
			// go-tex/engine has had to test for bad lines in a pass that reported ok.
			//
			// ⛔ (b) THE ARTIFICIAL CLAUSE IS REACHED ONLY WHEN THE LINE IS HOPELESS OR
			// THE BREAK IS FORCED. It sat outside the branch, so on a final pass it also
			// fired at an ordinary breakpoint whose line was merely past the threshold
			// (b>threshold, b<=inf_bad) — where TeX says `goto continue`: do not record,
			// keep the node active. Recording those at ZERO cost makes them beat every
			// real break, which is what arming FinalPass in go-tex/engine looked like
			// when it hyphenated eleven titles into "THETI-TLE".
			//
			// desperate is that branch's own test: b>inf_bad, or pi=eject_penalty.
			desperate := r < -1 || forced
			artificial := p.FinalPass && desperate && onlyActive && !recorded()
			if feasible || artificial {
				fit := fitness(r, order, lb)
				var d float64
				if artificial && !feasible {
					d = 0 // artificial_demerits
				} else {
					// ⛔⛔ THE PARAGRAPH-ENDING BREAK IS A DISTINGUISHED CALL, NOT THE LAST
					// ITERATION OF THE LOOP. tex.web:17197-17198,
					// @<Try the final line break at the end of the paragraph...@>:
					//
					//	begin try_break(eject_penalty,hyphenated);
					//
					// `break_type` is hyphenated UNCONDITIONALLY there, whatever item
					// actually sits at the end — a paragraph ends on \parfillskip and a
					// forced penalty, and a penalty is never a hyphen. So in
					//
					//	if (break_type=hyphenated)and(type(r)=hyphenated) then
					//	  if cur_p<>null then d:=d+double_hyphen_demerits
					//	  else d:=d+final_hyphen_demerits;      {:16906-16908}
					//
					// the test reduces at that break to "was the PREVIOUS break a hyphen",
					// and `cur_p=null` then picks final_hyphen_demerits.
					//
					// This read `flaggedAt(items, b)` and `b == n`, which is wrong twice over
					// and in a way that cancels into silence:
					//
					//   * `atEnd` was `b == n`, but the paragraph's last line ends at
					//     `paraEnd` — the forced penalty — so \finalhyphendemerits was
					//     UNREACHABLE and \doublehyphendemerits stood in for it
					//     (go-typeset/linebreak#12 item 3);
					//   * and `flagged` was the item's own flag, which at that same break is
					//     a \penalty-10000 and therefore false — so NEITHER was charged.
					//
					// The second hides the first: fixing `atEnd` alone changes nothing while
					// `flagged` is false there. A hyphen on the last line but one was free.
					atParaEnd := b == paraEnd
					flagged := flaggedAt(items, b) || atParaEnd
					d = demerits(lb, fit, penaltyAt(items, b), flagged, a, items, atParaEnd, p)
				}
				total := a.demerits + d
				// tex.web:16843-16845 prints HERE, inside "Record a new feasible
				// break" and before the champion comparison below: every feasible
				// break TeX considers reaches the log, not only the ones that win.
				if p.Trace != nil {
					fmt.Fprintf(p.Trace, "@%s via @@%d b=%s p=%d d=%s\n",
						traceKind(items, b, paraEnd), a.serial,
						traceBadness(r, lb), int(penaltyAt(items, b)),
						traceDemerits(d, artificial && !feasible))
				}
				si := slotAt(a.line + 1)
				if cur := slots[si].best[fit]; cur == nil || total < cur.demerits {
					slots[si].best[fit] = &breakNode{position: b, line: a.line + 1, fitness: fit, demerits: total, prev: a}
				}
			}
		}
		active = survivors
		// Not every class earns a node. \adjdemerits is the most a worse class could
		// ever save later, so a break already further behind than that can never be
		// chosen in the end and is dropped here (tex.web §16476-16487) — which is
		// also what stops the active list from growing fourfold.
		lowest := math.Inf(1)
		for i := range slots {
			for _, nd := range slots[i].best {
				if nd != nil && nd.demerits < lowest {
					lowest = nd.demerits
				}
			}
		}
		limit := lowest + math.Abs(p.AdjDemerits)
		// ⛔ IN SLOT ORDER, which is the order the line numbers were first reached,
		// so the active list and the trace do not depend on map iteration. A
		// breaker whose output moves between runs cannot be compared with anything.
		for i := range slots {
			for _, nd := range slots[i].best {
				if nd == nil || nd.demerits > limit {
					continue
				}
				// tex.web:16659-16674 prints here, as the active node is linked in, and
				// the serial is assigned at the same moment — which is why a feasible
				// break names its ancestor's serial and the start of the paragraph is
				// @@0 (break_node(r)=null there).
				serial++
				nd.serial = serial
				if p.Trace != nil {
					hyph := ""
					if flaggedAt(items, nd.position) || nd.position == paraEnd {
						hyph = "-"
					}
					fmt.Fprintf(p.Trace, "@@%d: line %d.%d%s t=%d -> @@%d\n",
						nd.serial, nd.line, nd.fitness, hyph, int(nd.demerits), nd.prev.serial)
				}
				active = append(active, nd)
				// ⛔⛔ THE PARAGRAPH'S LAST LINE ENDS AT b == n, NOT AT ANY FORCED BREAK.
				//
				// This read `if forced && …`, and a forced penalty in the MIDDLE of the
				// paragraph is forced too — so such a node could become the paragraph's
				// final node and everything after it was silently dropped. It did not bite
				// while a path was allowed to span the forced break and reach b == n with
				// lower demerits; closing that path (see the survivors above, tex.web
				// §16769) exposed it at once: a four-word fixture with a forced penalty in
				// the middle came back as ONE line ending at the penalty, the last two
				// words gone.
				//
				// tex.web §16054-16064: line_break finishes by following the active node
				// reached when cur_p is null — the end of the LIST. A break inside the
				// paragraph is an ancestor of that node, never a substitute for it.
				//
				// ⛔ And the end is `paraEnd`, the LAST forced penalty, not n. n is one past
				// the final item: choosing it adds an empty last line holding nothing but
				// the \parfillskip and the terminating penalty. Every existing test grew
				// exactly one such line when this said `b == n`.
				if b == paraEnd {
					// ⛔ EVERY ending is kept, not only the cheapest. \looseness chooses
					// among them (tex.web:17207-17237) and cannot choose from a list of
					// one. Keeping the minimum alone was invisible while Looseness was
					// unread, which is #12 item (7).
					finals = append(finals, nd)
					if best == nil || nd.demerits < best.demerits {
						best = nd
					}
				}
			}
		}
	}

	if best == nil {
		return nil, false
	}
	if p.Looseness != 0 {
		var matched bool
		best, matched = chooseLooseness(finals, best, p.Looseness)
		// tex.web:17203, verbatim in effect: the looseness has to be matched, or
		// this pass does not finish and a looser one runs — except on the final
		// pass, which takes the closest it reached.
		if !matched && !p.FinalPass {
			return nil, false
		}
	}
	// Reconstruct lines from the best final node.
	var rev []Line
	for nd := best; nd.prev != nil; nd = nd.prev {
		w, y, z := measure(nd.prev.position, nd.position)
		// ⛔ The SAME function measure() uses, for the reason written on it: a Start
		// reported for one span while the Ratio was measured on another describes two
		// different lines.
		start := lineStart(items, nd.prev.position, nd.position)
		// nd.line is this line's own number (it was set to prev.line+1 when the
		// node was made), so the SAME width the break was scored against is the
		// one the Ratio is reported for. Measuring one and reporting the other
		// is the mistake the comment on measure() warns about.
		indent, lineWidth := m.LineShape(nd.line)
		r, order := ratio(w, y, z, lineWidth)
		rev = append(rev, Line{Start: start, End: nd.position, Ratio: r, StretchOrder: order, Indent: indent})
	}
	// reverse
	lines := make([]Line, len(rev))
	for i := range rev {
		lines[i] = rev[len(rev)-1-i]
	}
	return lines, true
}

// lineStart is where the line after a break at a begins: the first item that a
// break does NOT discard.
//
// ⛔⛔ TeX DISCARDS A RUN, NOT AN ITEM, and this package used to discard exactly
// one glue. tex.web:16506-16511 says what break_width is:
//
//	The |break_width| is usually negative, since it consists of the background
//	… minus the width of nodes following~|cur_p| that are eliminated after a
//	break. If, for example, node |cur_p| is a glue node, the width of this glue
//	is subtracted from the background; and we also look ahead to eliminate all
//	subsequent glue and penalty and kern and math nodes, subtracting their
//	widths as well.
//
// and tex.web:16517-16534 is the loop, which this reproduces for the item kinds
// that exist here:
//
//	s:=cur_p;
//	while s<>null do
//	  begin if is_char_node(s) then goto done;
//	  case type(s) of
//	  glue_node:    <subtract glue from break_width>;
//	  penalty_node: do_nothing;
//	  math_node:    break_width[1]:=break_width[1]-width(s);
//	  kern_node:    if subtype(s)<>explicit then goto done
//	                else break_width[1]:=break_width[1]-width(s);
//	  othercases goto done
//	  endcases;
//	  s:=link(s);
//	  end;
//
// ⭐ NOTE THE PENALTY CASE: `do_nothing`, and then `s:=link(s)` all the same. A
// penalty does not stop the run, it is stepped over — so a break AT a penalty
// discards the glue that follows it, which one-item discarding kept. A penalty's
// own width is charged to the line that BREAKS there (measure adds it at the far
// end), never to the line that follows.
//
// ⚠ TWO OF TeX's FOUR CASES CANNOT BE REACHED FROM HERE YET, and saying so is
// part of the port. There is no KKern and no KMath: a consumer maps an explicit
// kern and a math-on/off node to KBox (go-typeset/linebreak#12 item 8 reports the
// same gap from the other side, for legal()), and a KBox ends the run. So the
// glue-and-penalty half of §837 is faithful here and the kern-and-math half is
// still unreachable — it needs the two item kinds, and that is an API addition,
// not a change to this function.
//
// ⛔ a <= 0 IS NOT A BREAK. The active list is seeded with position 0, the
// paragraph's beginning, where there is nothing to discard — tex.web:17001,
// `prev_p:=cur_p; {glue at beginning is not a legal breakpoint}`. Discarding there
// threw away \noindent\hskip 20pt's 20pt (go-tex/engine#657), which is what
// initialglue_test.go guards.
//
// limit stops the run at the break the line ENDS at, so a line made of nothing
// but discardables comes out empty (width = background) rather than negative.
// Two adjacent penalties are both legal breaks, so that list is reachable.
//
// ⛔ limit MUST be at most len(items), and there is deliberately no clamp.
// Both callers pass a break position — measure's `b`, the reconstruction's
// nd.position — and both are bounded by len(items) by construction. A clamp for
// the case neither can produce is a line no test can reach, which this package's
// 100% gate turns into a red build and rightly: it caught exactly that here. And
// a clamp is the wrong answer anyway — it would absorb a caller's out-of-range
// limit and return a quietly wrong start, where an index panic names the bug.
func lineStart(items []Item, a, limit int) int {
	if a <= 0 {
		return a
	}
	s := a
	for s < limit {
		switch items[s].Kind {
		case KGlue, KPenalty:
			s++
		default:
			return s // a box is tex.web's char node: `goto done`
		}
	}
	return s
}

// MaxBadRatio is the worst finite badness ratio the optimiser will consider: a
// line that is underfull with no stretch at all. Callers pass it as the tolerance
// for a last-resort pass that must return SOMETHING rather than fail.
// MaxBadRatio caps the adjustment ratio of a short line that has no stretch (the
// worst finite badness): the line is very bad but still finite, so an
// emergency pass with a large tolerance can accept it instead of collapsing the
// whole paragraph onto one line.
const MaxBadRatio = 1e4

// ratio is the glue adjustment ratio r for a line of natural width w with
// stretch y (by order) and shrink z, on a line of width L. It also returns the
// order the ratio is measured against.
//
// ⛔ ONLY THE HIGHEST ORDER PRESENT STRETCHES, and the lower ones keep their
// natural width — the rule hpack sets glue by. So the ratio is the shortfall
// over THAT order's total, and the caller is told which one it was.
//
// ⚠ Shrinking has one order (§16806), so a line being shrunk always reports
// OrderPt however much fil it also carries: fil can only pull a line wider.
func ratio(w float64, y [numOrders]float64, z, L float64) (float64, uint8) {
	switch {
	case w < L:
		o := highestOrder(y)
		if y[o] <= 0 {
			return MaxBadRatio, OrderPt // underfull with no stretch: bad but finite
		}
		return (L - w) / y[o], o
	case w > L:
		if z <= 0 {
			// ⛔⛔ NEGATIVE infinity. This returned +Inf — "impossibly bad", which is
			// true, but on the WRONG SIDE OF THE SCALE. Every caller asks `r >= -1` to
			// mean "this line is not overfull past its shrink", and +Inf passes that
			// test: an unshrinkable overfull line read as an infinitely STRETCHED one,
			// so its node stayed active and could still be chosen.
			//
			// tex.web §16767-16769 puts it the other way round: shrinking past the
			// available shrink gives b := inf_bad+1, and `if (b>inf_bad)…` then
			// deactivates the node. Overfull means out, not loose.
			//
			// Measured in go-tex/engine: `aaaaaaa bbbbbbb` at 73pt natural on a 40pt
			// measure came back as ONE line of ratio +Inf — 33pt over the margin,
			// reported as a decent line because the \parfillskip's infinite stretch
			// was read as covering it. badness() answers InfPenalty for either sign,
			// so the cost was right all along; only the sign of the ratio was wrong.
			return math.Inf(-1), OrderPt
		}
		return (L - w) / z, OrderPt // negative
	default:
		return 0, highestOrder(y)
	}
}

// highestOrder is the highest order with any stretch at all, or OrderPt when
// there is none. It is what decides which glues take up a line's slack.
func highestOrder(y [numOrders]float64) uint8 {
	for o := numOrders - 1; o > 0; o-- {
		if y[o] != 0 {
			return uint8(o)
		}
	}
	return OrderPt
}

// ── \tracingparagraphs ──────────────────────────────────────────────────────
//
// Three helpers, each reproducing one of tex.web's printing decisions rather
// than formatting a number the way Go would. See Params.Trace.

// traceKind is what TeX prints after the "@": nothing at all for a break at
// glue, the control sequence naming the node otherwise, and \par at the
// paragraph's end, where cur_p is null (tex.web:16860-16866).
//
// ⚠ TeX has five cases and this package can reach three: it is handed items,
// and a kern or a math node is not one of its kinds, so \kern and \math cannot
// arise. A consumer that maps those onto Box loses the distinction before the
// breaker sees it, which is worth knowing when diffing a log — the break is
// still there, labelled as glue.
func traceKind(items []Item, b, paraEnd int) string {
	switch {
	case b >= len(items) || b == paraEnd:
		return `\par`
	case items[b].Kind == KPenalty && items[b].Flagged:
		return `\discretionary`
	case items[b].Kind == KPenalty:
		return `\penalty`
	}
	return ""
}

// traceBadness prints "*" for a line whose badness is past inf_bad, and the
// badness itself otherwise (tex.web:16871).
//
// ⛔ THE STAR IS NOT "BADNESS ≥ 10000", AND TESTING THAT WOULD HAVE MADE IT DEAD
// CODE. §108 caps badness AT inf_bad, so a computed badness can equal 10000 and
// never exceed it — lineBadness here clamps the same way, and a `> 10000` test
// never fires. TeX's `b` reaches inf_bad+1 by assignment, in exactly one case,
// stated at :16753-16756:
//
//	This value will always be between zero and |inf_bad+1|; the latter value
//	occurs only in the case of lines from |r| to |cur_p| that cannot shrink
//	enough to fit the necessary width.
//
// and made at :16810, `if -shortfall>cur_active_width[6] then b:=inf_bad+1`.
// "Cannot shrink enough" is r < −1 here, so that is the test. Found by the test
// for this star failing on every bad line printing b=10000.
func traceBadness(r, b float64) string {
	if r < -1 {
		return "*"
	}
	return fmt.Sprintf("%d", int(b))
}

// traceDemerits prints "*" when the demerits were forced to zero by
// artificial_demerits and the number otherwise (tex.web:16874). The star is the
// point of it: it says the break was taken because the paragraph had no other
// way out, not because it was cheap.
func traceDemerits(d float64, artificial bool) string {
	if artificial {
		return "*"
	}
	return fmt.Sprintf("%d", int(d))
}

// infBadness is tex.web's inf_bad (§108): the badness of a line that is as bad
// as TeX will describe, and the threshold the trace marks with a star.
const infBadness = 10000

func penaltyAt(items []Item, b int) float64 {
	if b < len(items) && items[b].Kind == KPenalty {
		return items[b].Penalty
	}
	return 0
}

func flaggedAt(items []Item, b int) bool {
	return b < len(items) && items[b].Kind == KPenalty && items[b].Flagged
}

// fitness classifies a line by how hard its spaces worked, on the badness scale
// TeX uses (tex.web §16790-16812): stretching, badness over 99 is very loose and
// over 12 is loose; shrinking, badness over 12 is tight; everything else decent.
// Badness 12 is a ratio of about a half, 99 about one.
// ⛔ AN INFINITE ORDER IS `decent`, FULL STOP (tex.web §16790-16793):
//
//	if (cur_active_width[3]<>0)or(cur_active_width[4]<>0)or(cur_active_width[5]<>0)
//	  then begin b:=0; fit_class:=decent_fit; {infinite stretch} end
//
// — no badness is computed at all, so no amount of shortfall can make a line
// with fil on it loose. The order must be passed in; computing it from the
// ratio is impossible, because a ratio of 0.3 says nothing about which column
// the 0.3 was measured against.
//
// b is the line's badness as the caller computed it (Params.lineBad), so the
// class and the tolerance test are cut on the SAME number.
func fitness(r float64, order uint8, b float64) int {
	if order > OrderPt && r >= 0 {
		return decent
	}
	switch {
	case r < 0:
		if b > 12 {
			return tight
		}
	case b > 99:
		return veryLoose
	case b > 12:
		return loose
	}
	return decent
}

// demerits is the cost of one line (tex.web §16900-16910):
//
//	d := line_penalty + b;
//	if abs(d) >= 10000 then d := 100000000 else d := d*d;
//	if pi <> 0 then if pi > 0 then d := d + pi*pi
//	  else if pi > eject_penalty then d := d - pi*pi;
//	if (break_type = hyphenated) and (type(r) = hyphenated) then
//	  if cur_p <> null then d := d + double_hyphen_demerits
//	  else d := d + final_hyphen_demerits;
//	if abs(fit_class - fitness(r)) > 1 then d := d + adj_demerits;
//
// ⛔ `b` HERE IS THE SAME `b` §16790 COMPUTED, so an infinite order makes it 0
// and the line costs line_penalty² — the cheapest a line can be. Passing the
// order is not decoration: computing badness(r) again from the ratio would
// charge a \raggedright line for stretch it did not do, and since every line of
// such a paragraph would be charged alike the paragraph's SHAPE would still come
// out justified. That is #12 item (1) surviving in the cost function after being
// fixed in the feasibility test.
func demerits(b float64, fit int, pen float64, flagged bool, a *breakNode, items []Item, atEnd bool, p Params) float64 {
	base := p.LinePenalty + b
	var d float64
	if math.Abs(base) >= 10000 {
		d = 1e8 // TeX's ceiling: past this the line is hopeless, not merely bad
	} else {
		d = base * base
	}
	if pen > 0 && pen < InfPenalty {
		d += pen * pen
	} else if pen > -InfPenalty && pen < 0 {
		d -= pen * pen
	}
	if flagged && a.position > 0 && a.position < len(items) &&
		items[a.position].Kind == KPenalty && items[a.position].Flagged {
		// Two hyphenated lines running: the last line but one costs less, because
		// TeX would rather hyphenate there than leave the paragraph ragged.
		if atEnd {
			d += p.FinalHyph
		} else {
			d += p.DoubleHyph
		}
	}
	if fit-a.fitness > 1 || a.fitness-fit > 1 {
		d += p.AdjDemerits
	}
	return d
}

// lineBadness is badness with §16790's infinite-stretch case in front of it:
// any fil/fill/filll on a line being STRETCHED makes b zero, whatever the ratio.
// A line being shrunk (r < 0) has no infinite case, because shrinkability is
// never infinite in a paragraph (§16806).
func lineBadness(r float64, order uint8) float64 {
	if order > OrderPt && r >= 0 {
		return 0
	}
	return badness(r)
}

// lineBad is the badness of the line from a to b, on the scale the caller chose.
//
// By default that is the float 100·|r|³ (lineBadness). With
// Params.ScaledPointsPerUnit it is tex.web §108's integer badness(t,s), computed
// from the widths themselves — t the shortfall (or excess), s the stretch of the
// order that sets the line (or the shrink) — because r = t/s in floating point
// has already lost what §108 rounds: the float and §108 disagree on a band of
// shortfalls around every decision boundary (12, 99, \tolerance), and the band
// is not a single sp.
func (p Params) lineBad(w float64, y [numOrders]float64, z, L, r float64, order uint8) float64 {
	k := p.ScaledPointsPerUnit
	if k <= 0 || order > OrderPt && r >= 0 || r < -1 || r >= MaxBadRatio {
		return lineBadness(r, order)
	}
	if r >= 0 {
		return float64(TeXBadness(int(math.Round((L-w)*k)), int(math.Round(y[order]*k))))
	}
	return float64(TeXBadness(int(math.Round((w-L)*k)), int(math.Round(z*k))))
}

// TeXBadness is tex.web §108's badness(t,s): how hard glue of stretchability (or
// shrinkability) s must work to take up a shortfall (or excess) t, as an integer
// from 0 to inf_bad (10000). t and s are scaled points.
//
//	function badness(t,s:scaled):halfword; {compute badness, given t>=0}
//	begin if t=0 then badness:=0
//	else if s<=0 then badness:=inf_bad
//	else  begin if t<=7230584 then r:=(t*297) div s
//	  else if s>=1663497 then r:=t div (s div 297)
//	  else r:=t;
//	  if r>1290 then badness:=inf_bad
//	  else badness:=(r*r*r+@'400000) div @'1000000;
//	  end;
//	end;
//
// The three branches are overflow guards for 32-bit Pascal integers, and they
// change results, so they are kept: 297³ = 99.94·2¹⁸ scales the cube to 100·(t/s)³,
// 7230584 is where t·297 would overflow, 1290³ < 2³¹ < 1291³. @'400000 and
// @'1000000 are OCTAL: 2¹⁷ (the rounding half) and 2¹⁸. A negative t is the caller's
// error (§108 says "given t>=0"), and answers inf_bad rather than a plausible number.
func TeXBadness(t, s int) int {
	switch {
	case t < 0:
		return int(InfPenalty)
	case t == 0:
		return 0
	case s <= 0:
		return int(InfPenalty)
	}
	var r int64
	switch {
	case t <= 7230584:
		r = int64(t) * 297 / int64(s)
	case s >= 1663497:
		r = int64(t) / (int64(s) / 297)
	default:
		r = int64(t)
	}
	if r > 1290 {
		return int(InfPenalty)
	}
	return int((r*r*r + 0o400000) / 0o1000000)
}

// badness is 100·|r|³, capped: how far a line's spaces had to stretch or shrink,
// cubed so that one very bad line costs more than several mediocre ones.
func badness(r float64) float64 {
	if math.IsInf(r, 0) {
		return InfPenalty
	}
	b := 100 * math.Abs(r) * math.Abs(r) * math.Abs(r)
	if b > InfPenalty {
		return InfPenalty
	}
	return b
}
