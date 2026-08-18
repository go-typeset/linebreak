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
	demerits float64    // total demerits of the best path to here
	prev     *breakNode // the breakpoint that begins this line
}

// KnuthPlass breaks items into lines of the given width, minimising total
// cost (linePenalty is charged once per line, which discourages breaking a
// paragraph into many short ones). It returns the chosen lines in
// order and ok=false if no sequence of feasible breaks exists within tolerance.
func KnuthPlass(items []Item, lineWidth, tolerance, linePenalty float64) ([]Line, bool) {
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
	measure := func(a, b int) (w, y, z float64) {
		start := a
		if a < n && items[a].Kind == KGlue {
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

	active := []*breakNode{{position: 0, line: 0}}
	var best *breakNode

	for b := 1; b <= n; b++ {
		if !legal(b) {
			continue
		}
		forced := b == n || (items[b].Kind == KPenalty && items[b].Penalty <= -InfPenalty)
		var survivors []*breakNode
		var bestHere *breakNode
		for _, a := range active {
			w, y, z := measure(a.position, b)
			r := ratio(w, y, z, lineWidth)
			// A node stays active for *later* breaks only while the line to here is
			// not already overfull past its shrink (r ≥ −1).
			if r >= -1 {
				survivors = append(survivors, a)
			}
			// A break is taken from a if the line is feasible, or if the break is
			// forced (then even an over/underfull line must close the paragraph).
			if (r >= -1 && r <= tolerance) || forced {
				d := demerits(r, penaltyAt(items, b), flaggedAt(items, b), a, items, linePenalty)
				total := a.demerits + d
				if bestHere == nil || total < bestHere.demerits {
					bestHere = &breakNode{position: b, line: a.line + 1, demerits: total, prev: a}
				}
			}
		}
		active = survivors
		if bestHere != nil {
			active = append(active, bestHere)
			if forced && (best == nil || bestHere.demerits < best.demerits) {
				best = bestHere
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
		start := nd.prev.position
		if start < n && items[start].Kind == KGlue {
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
			return math.Inf(1) // cannot shrink at all → treat as impossibly bad
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

// demerits is the cost of one line: (linePenalty + badness)² adjusted by the
// breakpoint penalty and a flagged-break penalty.
func demerits(r, pen float64, flagged bool, a *breakNode, items []Item, linePenalty float64) float64 {
	b := badness(r)
	base := linePenalty + b
	d := base * base
	if pen > 0 && pen < InfPenalty {
		d += pen * pen
	} else if pen > -InfPenalty && pen < 0 {
		d -= pen * pen
	}
	if flagged && a.position > 0 && a.position < len(items) &&
		items[a.position].Kind == KPenalty && items[a.position].Flagged {
		d += 10000 // consecutive flagged (double-hyphen) penalty
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
