// Copyright (c) the go-typeset authors.
// SPDX-License-Identifier: BSD-3-Clause

package linebreak

// A Measure gives a paragraph its shape: the left margin and the width
// available to each line. TeX recomputes both FOR EVERY LINE — the width is an
// input to the badness of every candidate break, not a constant of the
// paragraph — which is what makes \hangindent and \parshape line-breaking
// parameters rather than placement ones (tex.web §847-850, lines 16676-16748).
//
// ⛔ A SCALAR MEASURE CANNOT EXPRESS EITHER OF THEM, and the consequence is not
// only a missing indent: four ems more text on the first line re-breaks the
// whole paragraph, so a phrase that lands on the reference's line 4 lands on
// line 3 instead. Lines gained or lost at the end of a paragraph become pages
// gained or lost (go-typeset/linebreak#11, from a consumer measuring against
// tectonic).
type Measure interface {
	// LineShape reports the left margin and the width available to line n of
	// the paragraph, numbered from 1.
	//
	// ⚠ ONE METHOD RETURNING BOTH, deliberately. tex.web computes them
	// together from the same three parameters and the sign rules couple them:
	// a negative \hangindent shortens the line and leaves the margin at zero,
	// so a caller that derived the margin separately would have to restate
	// that rule and could get it wrong on its own. Breaking needs only the
	// width; the margin is here so the caller that places the line does not
	// have to re-derive it.
	LineShape(n int) (indent, width float64)
}

// Uniform is TeX's normal setting — par_shape_ptr null, hang_indent 0 — where
// every line has the same width and no margin (tex.web:16706-16710:
// "last_special_line:=0; second_width:=hsize; second_indent:=0").
func Uniform(hsize float64) Measure { return uniform(hsize) }

type uniform float64

func (u uniform) LineShape(int) (float64, float64) { return 0, float64(u) }

// HangIndent is \hangindent with \hangafter, read from tex.web:16719-16732
// rather than from the usual description of it:
//
//	last_special_line:=abs(hang_after);
//	if hang_after<0 then
//	  begin first_width:=hsize-abs(hang_indent);
//	  if hang_indent>=0 then first_indent:=hang_indent
//	  else first_indent:=0;
//	  second_width:=hsize; second_indent:=0;
//	  end
//	else  begin first_width:=hsize; first_indent:=0;
//	  second_width:=hsize-abs(hang_indent);
//	  if hang_indent>=0 then second_indent:=hang_indent
//	  else second_indent:=0;
//	  end;
//
// So two rules that are easy to get wrong, and are therefore encoded once here:
//
//   - ⛔ THE SIGN OF indent IS NOT THE SIGN OF THE MARGIN. The line is always
//     shortened by |indent|; the margin is indent only when it is positive, and
//     0 when it is negative. A negative \hangindent indents from the RIGHT.
//   - ⛔ after < 0 MEANS "THE FIRST |after| LINES", not "after none of them":
//     the indented lines are 1..|after|, where a non-negative after indents
//     lines after+1, after+2, …
//
// tex.web:16689 also notes that "if hang_indent=0, the value of hang_after is
// irrelevant", which falls out of the arithmetic here rather than being special-cased.
func HangIndent(hsize, indent float64, after int) Measure {
	h := hang{special: after, hsize: hsize}
	narrow := hsize - abs(indent)
	margin := 0.0
	if indent >= 0 {
		margin = indent
	}
	if after < 0 {
		h.special = -after
		h.firstWidth, h.firstIndent = narrow, margin
		h.restWidth, h.restIndent = hsize, 0
	} else {
		h.firstWidth, h.firstIndent = hsize, 0
		h.restWidth, h.restIndent = narrow, margin
	}
	return h
}

type hang struct {
	special                 int // last_special_line: abs(hang_after)
	hsize                   float64
	firstWidth, firstIndent float64
	restWidth, restIndent   float64
}

func (h hang) LineShape(n int) (float64, float64) {
	if n > h.special {
		return h.restIndent, h.restWidth
	}
	return h.firstIndent, h.firstWidth
}

// A Shape is one \parshape pair: a left margin and a width.
type Shape struct{ Indent, Width float64 }

// ParShape is \parshape: an explicit margin and width per line.
//
// ⛔ THE LAST PAIR REPEATS FOR EVERY LINE BEYOND THE LIST, which is why a
// Measure is an interface and not a slice — the sequence is unbounded.
// tex.web:16680-16681: "the other 2n words contain the left margins and line
// lengths for the first n lines of the paragraph; the specifications for line n
// apply to all subsequent lines."
//
// An empty list is Uniform(0): \parshape=0 is how TeX turns the shape off, and
// the caller that means "no shape" should say Uniform.
func ParShape(lines []Shape) Measure {
	return parshape(append([]Shape(nil), lines...)) // copy: the caller keeps its slice
}

type parshape []Shape

func (ps parshape) LineShape(n int) (float64, float64) {
	if len(ps) == 0 {
		return 0, 0
	}
	if n < 1 {
		n = 1
	}
	if n > len(ps) {
		n = len(ps) // the last pair applies to every line beyond the list
	}
	return ps[n-1].Indent, ps[n-1].Width
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
