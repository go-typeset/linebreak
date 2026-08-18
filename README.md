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

## Tests

`go test ./...` — 100% statement coverage, run on six 64-bit architectures
(amd64, arm64, riscv64, loong64, ppc64le, s390x), three operating systems, and
both wasm targets.

## Licence

BSD-3-Clause.
