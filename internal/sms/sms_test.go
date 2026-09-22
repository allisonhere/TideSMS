package sms

import (
	"strings"
	"testing"
)

func TestGSM7SingleAndMulti(t *testing.T) {
	if e := Count("hello"); e.Units != 5 || e.Segments != 1 || e.Remaining != 155 || e.Unicode {
		t.Fatalf("%+v", e)
	}
	long := strings.Repeat("a", 161)
	e := Count(long)
	if e.Segments != 2 || e.Units != 161 {
		t.Fatalf("161 gsm chars = %+v", e)
	}
	if got := Count(strings.Repeat("a", 160)); got.Segments != 1 || got.Remaining != 0 {
		t.Fatalf("exactly 160 = %+v", got)
	}
	if got := Count(strings.Repeat("a", 306)); got.Segments != 2 {
		t.Fatalf("306 = %+v", got)
	}
	if got := Count(strings.Repeat("a", 307)); got.Segments != 3 {
		t.Fatalf("307 = %+v", got)
	}
}

func TestGSM7ExtendedCountsDouble(t *testing.T) {
	e := Count("€")
	if e.Unicode || e.Units != 2 {
		t.Fatalf("euro should be two septets: %+v", e)
	}
	e = Count("{")
	if e.Units != 2 {
		t.Fatalf("brace = %+v", e)
	}
}

func TestUnicodeForcesUCS2(t *testing.T) {
	e := Count("héllo 😄")
	if !e.Unicode {
		t.Fatal("emoji should force UCS-2")
	}
	if e.Segments != 1 {
		t.Fatalf("short unicode = %+v", e)
	}
	if got := Count(strings.Repeat("日", 71)); got.Segments != 2 || !got.Unicode {
		t.Fatalf("71 CJK = %+v", got)
	}
	if got := Count(strings.Repeat("日", 70)); got.Segments != 1 {
		t.Fatalf("70 CJK = %+v", got)
	}
}

func TestAstralCountsTwoUnits(t *testing.T) {
	// A non-BMP emoji is two UTF-16 code units.
	if e := Count("😀"); !e.Unicode || e.Units != 2 {
		t.Fatalf("emoji units = %+v", e)
	}
}
