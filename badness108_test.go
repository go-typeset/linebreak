package linebreak

import (
	"math/rand"
	"testing"
)

// tex.web §108, by hand from the Pascal: (t*297) div s, then (r³+2¹⁷) div 2¹⁸.
// The 65536-based rows are the values TeX prints for badness of whole points.
func TestTeXBadnessTable(t *testing.T) {
	const pt = 65536
	for _, c := range []struct {
		name string
		t, s int
		want int
	}{
		{"nothing to take up", 0, 100, 0},
		{"nothing to take up, no glue", 0, 0, 0},
		{"no stretch at all", 90 * pt, 0, 10000},
		{"negative stretch", 90 * pt, -5, 10000},
		{"t = s is 100", 90 * pt, 90 * pt, 100},
		{"80 of 90 pt", 80 * pt, 90 * pt, 70},
		{"10 of 20 pt", 10 * pt, 20 * pt, 12},
		// r = 148 → (148³+2¹⁷) div 2¹⁸ = 12; r = 149 → 13.
		{"r=148 is 12", 49600, 100000, 12},
		{"r=149 is 13", 50200, 100000, 13},
		// r = 297 is exactly t = s: 297³ = 26198073 → 100.
		{"r=297", 297, 297, 100},
		// 1290 is the last r whose cube fits; 1291 and up is inf_bad.
		// t=4344,s=1000 → r=1290; (1290³+2¹⁷) div 2¹⁸ = 8189. t=4347 → r=1291.
		{"r=1290 is 8189", 4344, 1000, 8189},
		{"r=1291 is inf_bad", 4347, 1000, 10000},
		// t beyond 7230584: second branch r = t div (s div 297).
		{"large t, large s", 10_000_000, 20_000_000, 12},
		// t beyond 7230584 and s < 1663497: r = t, so inf_bad.
		{"large t, small s", 8_000_000, 1_000_000, 10000},
		{"negative t is the caller's error", -1, 10, 10000},
	} {
		if got := TeXBadness(c.t, c.s); got != c.want {
			t.Errorf("%s: TeXBadness(%d,%d) = %d, want %d", c.name, c.t, c.s, got, c.want)
		}
	}
}

func texFirst(s int, pred func(t, s int) bool) int {
	lo, hi := 0, 4*s+2
	for lo < hi {
		m := (lo + hi) / 2
		if pred(m, s) {
			hi = m
		} else {
			lo = m + 1
		}
	}
	return lo
}

// MEASURED, not assumed: the float 100·(t/s)³ and §108 disagree about which side
// of 12, 99 and 200 a line falls on, over a band of shortfalls around each. This
// pins that the band exists (so ScaledPoints is not a no-op) and that §108 is
// monotone in t, which the bisection relies on. Last run: threshold 12, 26563 of
// 26600 stretch values disagree, worst band 566426 sp.
func TestFloatAndTeXBadnessDisagreeAtTheBoundaries(t *testing.T) {
	var ss []int
	for s := 1; s <= 5000; s++ {
		ss = append(ss, s)
	}
	for k := 1; k <= 400; k++ {
		ss = append(ss, k*65536, k*65536+1, k*65536-1, k*32768+k)
	}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		ss = append(ss, 1+rng.Intn(1<<26))
	}
	for _, k := range []int{12, 99, 200} {
		disagree := 0
		for _, s := range ss {
			ti := texFirst(s, func(t, s int) bool { return TeXBadness(t, s) > k })
			tf := texFirst(s, func(t, s int) bool { return badness(float64(t)/float64(s)) > float64(k) })
			if ti != tf {
				disagree++
			}
		}
		if disagree == 0 {
			t.Errorf("threshold %d: float and §108 never disagree; ScaledPoints would change nothing", k)
		}
		t.Logf("threshold %3d: %d of %d stretch values disagree", k, disagree, len(ss))
	}
}

// A line stretched by 0.495 of its stretch is badness 12.13 as a float — loose,
// and over a \tolerance of 12 — but §108 rounds it to 12: decent, and within 12.
// tex.web decides on the integer, so with ScaledPoints the paragraph sets.
func TestScaledPointsDecideOnTheIntegerBadness(t *testing.T) {
	items := []Item{
		Box(20000), Glue(10000, 100000, 0), Box(20000), // line 1: natural 50000, stretch 100000
		Glue(10000, 100000, 0), Box(60000), // cannot join line 1: no shrink
		Penalty(0, InfPenalty, false), GlueOrder(0, 1, OrderFil, 0),
	}
	const L = 99500 // shortfall 49500 of 100000: r = 0.495
	p := DefaultParams(12, 0)
	if _, ok := KnuthPlassWith(items, L, p); ok {
		t.Fatal("float badness 12.13 was accepted at tolerance 12")
	}
	p.ScaledPointsPerUnit = 1
	lines, ok := KnuthPlassWith(items, L, p)
	if !ok {
		t.Fatal("§108 badness 12 was refused at tolerance 12")
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
}

// The same fixture in TeX points (sp/65536, exact in binary): the unit is
// declared, not assumed, so a consumer that hands over points gets §108 too.
func TestScaledPointsPerUnitConvertsPoints(t *testing.T) {
	const pt = 65536.0
	items := []Item{
		Box(20000 / pt), Glue(10000/pt, 100000/pt, 0), Box(20000 / pt),
		Glue(10000/pt, 100000/pt, 0), Box(60000 / pt),
		Penalty(0, InfPenalty, false), GlueOrder(0, 1, OrderFil, 0),
	}
	p := DefaultParams(12, 0)
	p.ScaledPointsPerUnit = pt
	if _, ok := KnuthPlassWith(items, 99500/pt, p); !ok {
		t.Fatal("§108 badness 12 was refused at tolerance 12, widths in points")
	}
}

// The same on the shrinking side: excess 49500 of a shrink of 100000.
func TestScaledPointsDecideOnTheIntegerBadnessWhenShrinking(t *testing.T) {
	items := []Item{
		Box(100000), Glue(10000, 0, 100000), Box(100000), // natural 210000, shrink 100000
		Glue(10000, 0, 0), Box(100000),
		Penalty(0, InfPenalty, false), GlueOrder(0, 1, OrderFil, 0),
	}
	const L = 160500 // excess 49500: r = -0.495
	p := DefaultParams(12, 0)
	if _, ok := KnuthPlassWith(items, L, p); ok {
		t.Fatal("float badness 12.13 was accepted at tolerance 12")
	}
	p.ScaledPointsPerUnit = 1
	if _, ok := KnuthPlassWith(items, L, p); !ok {
		t.Fatal("§108 badness 12 was refused at tolerance 12")
	}
}
