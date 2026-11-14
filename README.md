# linebreak

[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests)

**Optimal line breaking for Go — pure Go, no cgo, no dependencies.**

Give it a sequence of boxes, glue and penalties and a line width; it returns the
breakpoints that minimise the total cost of the paragraph.

## Why not just fill lines greedily

A greedy breaker decides each line without looking ahead, so one tight line early
forces a bad one later, and the ragged edge wanders. This one treats the
paragraph as a whole: every legal breakpoint is a node, every candidate line is
an edge weighted by how far its spaces had to stretch or shrink, and the answer
is the path of least total cost. Hyphenation points, forced breaks and
discouraged breaks all enter as penalties, so a caller controls the outcome
without special cases.

The algorithm is Knuth and Plass, *Breaking Paragraphs into Lines* (1981) — the
same one behind the paragraphs people find noticeably even.

Useful wherever text is laid out and the result is looked at: PDF generation,
e-book rendering, terminal and console formatting, SVG text, a UI toolkit's
text layout.

## Use

```go
import "github.com/go-typeset/linebreak"

items := []linebreak.Item{
    linebreak.Box(30),                  // a word, 30 units wide
    linebreak.Glue(10, 5, 3),           // a space: 10 wide, may stretch 5, shrink 3
    linebreak.Box(45),
    // End a paragraph the way TeX does (tex.web §16077-16084): an INFINITE penalty,
    // then the finishing glue, then the forced break. The infinite penalty is not
    // decoration — §16070 says the \parfillskip "is preceded by an infinite penalty,
    // so it will never be considered as a potential breakpoint". Leave it out and that
    // glue IS a legal breakpoint (a glue after a box), which offers one break TeX never
    // offers, right at the end. A trailing glue should be REPLACED by the penalty
    // rather than kept, since "spaces usually precede blank lines" (§16068).
    linebreak.Penalty(0, linebreak.InfPenalty, false),
    linebreak.Glue(0, 1e6, 0),
    linebreak.Penalty(0, -linebreak.InfPenalty, false), // forced break: end of paragraph
}

lines, ok := linebreak.KnuthPlass(items, 100 /*line width*/, 1 /*tolerance*/, 10 /*line penalty*/)
if !ok {
    // no set of breaks fits within the tolerance; retry with linebreak.MaxBadRatio
}
for _, l := range lines {
    // l.Start, l.End index into items; l.Ratio is the space adjustment
    // (>0 stretched, <0 shrunk, 0 exact).
}
```

`Glyph(r, w, h, d)` is a box that also carries its rune and vertical metrics, for
callers that draw what they measure.

**Units are yours.** The algorithm never looks at what a width means — points,
pixels, ems or terminal columns all work, as long as one paragraph is consistent.

**A forced break is mandatory.** A `Penalty` of `−InfPenalty` is not a strong
preference: every active node is deactivated there (tex.web §16769), so no line spans
it. The paragraph's last line ends at the **last** forced penalty in the list.

**But mandatory is not unconditional, and a pass can fail at one.** tex.web §16769
reads, in full:

```pascal
if (b>inf_bad)or(pi=eject_penalty) then
  begin if final_pass and (minimum_demerits=awful_bad) and
     (link(r)=last_active) and (prev_r=active) then
    artificial_demerits:=true
  else if b>threshold then goto deactivate;
  node_r_stays_active:=false;
  end
else begin prev_r:=r; if b>threshold then goto continue; … end;
```

`goto deactivate` **skips the recording**. A forced break whose line is past the
tolerance is therefore not taken, and a pass that has no other break *fails* — which is
how TeX reaches its next pass. Up to v1.3.0 this package recorded every forced break
however badly its line fitted, so such a pass returned `ok` with a bad line in it and a
caller had to inspect the lines to find out. ⛔ **A caller that relied on that must now
run the next pass**, which is what tex.web does and what the parameters are for.

A line a `\\` ends carries `\hfil` (LaTeX's `\\` is `\unskip \nobreak \hfil \break`), so
in real input a forced break's line is feasible at any tolerance. Emit that glue: a
forced penalty after rigid material is input TeX would not set either.

**An overfull line's ratio is negative.** `Ratio` below −1 means the line is longer than
the measure by more than its shrink can absorb, and `−Inf` means it cannot shrink at
all. The sign is the contract: `r >= -1` is the test for "this line can be set", and an
unshrinkable overfull line must fail it. An underfull line with nothing to stretch is
`MaxBadRatio`, deliberately **finite** — TeX gives it `b := inf_bad`, which does not
exceed `inf_bad`, so such a line is bad but still usable.

**`Params.FinalPass`** arms TeX's `artificial_demerits` (§16824-16831): the break is
taken at **zero** cost instead of the node being deactivated, when all four of these
hold — and the first is the one that is easy to drop:

1. the line is **hopeless or the break is forced** (`b > inf_bad`, here `Ratio < −1`, or
   `pi = eject_penalty`). The clause lives *inside* that branch. At an ordinary
   breakpoint merely past the tolerance tex.web says `goto continue`: do not record,
   keep the node active;
2. nothing feasible has been recorded at this breakpoint;
3. it is the only active node;
4. `FinalPass` is set.

⛔ **Set it only on a genuine *final* pass**, after the earlier passes failed, and keep
the tolerance as it was — raising the tolerance makes a hopeless line feasible at its
full cost, which defeats the clause. A caller that arms it on every pass, or that drops
condition (1), will see breaks TeX never takes.

⚠ **And "final" may be the second pass.** tex.web §863 sets
`final_pass := (emergency_stretch <= 0)` when it enters the second pass: with no
`\emergencystretch` there is no third pass and the clause is live on the second. A
caller that keeps `FinalPass` for a pass it never runs has no final pass at all.

⚠ **What the clause does is permit an overfull line, not prefer short ones.** On both
fixtures in `finalpass_test.go`, tectonic 0.17.0 sets **one overfull line** rather than
breaking: 33.0pt too wide on the first, 21.5pt on the second. The README said the
opposite up to v1.3.0, on an argument from demerits that was never put to the
reference.

## Tests

`go test ./...` — 100% statement coverage, run on six 64-bit architectures
(amd64, arm64, riscv64, loong64, ppc64le, s390x), three operating systems, and
both wasm targets.

## Licence

BSD-3-Clause.
