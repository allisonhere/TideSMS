package search

import (
	"strings"
	"testing"
	"time"
)

func TestParseSeparatesFiltersFromText(t *testing.T) {
	q := Parse(`from:Amy before:2026-09-01 after:2026-08-01 dinner plans`)
	if q.Text != "dinner plans" {
		t.Fatalf("text = %q", q.Text)
	}
	if q.From != "Amy" {
		t.Fatalf("from = %q", q.From)
	}
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	if !q.Before.Equal(want) {
		t.Fatalf("before = %v", q.Before)
	}
	if !q.After.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("after = %v", q.After)
	}
}

func TestParseKeepsQuotedPhrase(t *testing.T) {
	q := Parse(`"dentist appointment"`)
	// The quotes are kept in Text so MatchExpression still sees one phrase.
	if q.Text != `"dentist appointment"` {
		t.Fatalf("phrase = %q", q.Text)
	}
	if got := MatchExpression(q.Text); got != `"dentist appointment"` {
		t.Fatalf("match = %q", got)
	}
}

func TestMatchExpressionQuotesEachTerm(t *testing.T) {
	got := MatchExpression(`dinner OR "drop table"`)
	// OR is a literal term here, not FTS syntax.
	for _, want := range []string{`"dinner"`, `"OR"`, `"drop table"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q missing from %q", want, got)
		}
	}
}

func TestEmptyAndFilterOnly(t *testing.T) {
	if !Parse("").Empty() {
		t.Fatal("blank query should be empty")
	}
	q := Parse("from:Amy")
	if !q.FilterOnly() || q.Empty() {
		t.Fatalf("from-only should be a filter: text=%q empty=%v", q.Text, q.Empty())
	}
	if MatchExpression("from:Amy") == "" {
		t.Fatal("free text should still tokenize")
	}
}

func TestInvalidDateIgnored(t *testing.T) {
	q := Parse("before:not-a-date hello")
	if !q.Before.IsZero() || q.Text != "hello" {
		t.Fatalf("bad date not ignored: %+v", q)
	}
}
