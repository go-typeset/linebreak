// Copyright (c) the go-typeset/linebreak authors.
// SPDX-License-Identifier: BSD-3-Clause

package linebreak

import "math"

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
	Penalty         float64 // penalty only
	Flagged         bool    // penalty only (e.g. a hyphen) — consecutive flags are penalised
}

// Box, Glue and Penalty are constructors.
func Box(w float64) Item { return Item{Kind: KBox, Width: w} }

// Glyph is a box carrying a rune and its height/depth (used by the typesetter).
func Glyph(r rune, w, h, d float64) Item {
	return Item{Kind: KBox, Width: w, Height: h, Depth: d, R: r}
}
func Glue(w, stretch, shrink float64) Item {
	return Item{Kind: KGlue, Width: w, Stretch: stretch, Shrink: shrink}
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
}

// breakNode is an active breakpoint in the dynamic program.
type breakNode struct {
	position int        // item index of this breakpoint
	line     int        // number of the line ending here
	fitness  int        // fitness class of the line ending here (veryLoose…tight)
	demerits float64    // total demerits of the best path to here
	prev     *breakNode // the breakpoint that begins this line
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
	Tolerance   float64 // \tolerance: the worst BADNESS a line may have (100·ratio³)
	LinePenalty float64 // \linepenalty: charged once per line, so fewer lines cost less
	AdjDemerits float64 // \adjdemerits: adjacent lines two fitness classes apart
	DoubleHyph  float64 // \doublehyphendemerits: two hyphenated lines in a row
	FinalHyph   float64 // \finalhyphendemerits: a hyphen on the last line but one

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
func KnuthPlassWith(items []Item, lineWidth float64, p Params) ([]Line, bool) {
	// Prefix sums of width/stretch/shrink over items strictly before index i, so
	// the material of a line from break a to break b is sums[b]−sums[a].
	n := len(items)
	sumW := make([]float64, n+1)
	sumY := make([]float64, n+1)
	sumZ := make([]float64, n+1)
	for i, it := range items {
		sumW[i+1] = sumW[i] + it.Width
		if it.Kind == KGlue {
			sumY[i+1] = sumY[i] + it.Stretch
			sumZ[i+1] = sumZ[i] + it.Shrink
		} else {
			sumY[i+1] = sumY[i]
			sumZ[i+1] = sumZ[i]
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
	// The same test is made twice — here and where the lines are rebuilt — so both
	// move together; fixing one would leave the Ratio measured on one span and the
	// Start reported for another.
	measure := func(a, b int) (w, y, z float64) {
		start := a
		if a > 0 && a < n && items[a].Kind == KGlue {
			start = a + 1 // discard the glue we broke at
		}
		w = sumW[b] - sumW[start]
		y = sumY[b] - sumY[start]
		z = sumZ[b] - sumZ[start]
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
		var bestHere [4]*breakNode
		// onlyActive mirrors TeX's "link(r)=last_active and prev_r=active": r is the
		// one and only active node, so deactivating it loses the paragraph.
		onlyActive := len(active) == 1
		for _, a := range active {
			w, y, z := measure(a.position, b)
			r := ratio(w, y, z, lineWidth)
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
			feasible := r >= -1 && badness(r) <= p.Tolerance
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
			artificial := p.FinalPass && desperate && onlyActive &&
				bestHere[0] == nil && bestHere[1] == nil && bestHere[2] == nil && bestHere[3] == nil
			if feasible || artificial {
				fit := fitness(r)
				var d float64
				if artificial && !feasible {
					d = 0 // artificial_demerits
				} else {
					d = demerits(r, fit, penaltyAt(items, b), flaggedAt(items, b), a, items, b == n, p)
				}
				total := a.demerits + d
				if cur := bestHere[fit]; cur == nil || total < cur.demerits {
					bestHere[fit] = &breakNode{position: b, line: a.line + 1, fitness: fit, demerits: total, prev: a}
				}
			}
		}
		active = survivors
		// Not every class earns a node. \adjdemerits is the most a worse class could
		// ever save later, so a break already further behind than that can never be
		// chosen in the end and is dropped here (tex.web §16476-16487) — which is
		// also what stops the active list from growing fourfold.
		lowest := math.Inf(1)
		for _, nd := range bestHere {
			if nd != nil && nd.demerits < lowest {
				lowest = nd.demerits
			}
		}
		limit := lowest + math.Abs(p.AdjDemerits)
		for _, nd := range bestHere {
			if nd == nil || nd.demerits > limit {
				continue
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
			if b == paraEnd && (best == nil || nd.demerits < best.demerits) {
				best = nd
			}
		}
	}

	if best == nil {
		return nil, false
	}
	// Reconstruct lines from the best final node.
	var rev []Line
	for nd := best; nd.prev != nil; nd = nd.prev {
		w, y, z := measure(nd.prev.position, nd.position)
		// ⛔ start > 0 for the reason written on measure: the paragraph's first line
		// begins where the paragraph begins, glue and all.
		start := nd.prev.position
		if start > 0 && start < n && items[start].Kind == KGlue {
			start++
		}
		rev = append(rev, Line{Start: start, End: nd.position, Ratio: ratio(w, y, z, lineWidth)})
	}
	// reverse
	lines := make([]Line, len(rev))
	for i := range rev {
		lines[i] = rev[len(rev)-1-i]
	}
	return lines, true
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
// stretch y and shrink z, on a line of width L.
func ratio(w, y, z, L float64) float64 {
	switch {
	case w < L:
		if y <= 0 {
			return MaxBadRatio // underfull with no stretch: bad but finite
		}
		return (L - w) / y
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
			return math.Inf(-1)
		}
		return (L - w) / z // negative
	default:
		return 0
	}
}

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
func fitness(r float64) int {
	b := badness(r)
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
func demerits(r float64, fit int, pen float64, flagged bool, a *breakNode, items []Item, atEnd bool, p Params) float64 {
	base := p.LinePenalty + badness(r)
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
