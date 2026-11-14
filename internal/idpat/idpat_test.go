package idpat

import (
	"slices"
	"testing"
)

func builtin(t *testing.T) *Matcher {
	t.Helper()
	m, err := New(Builtin())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestWordBoundaries(t *testing.T) {
	m := builtin(t)
	cases := []struct {
		line, id string
		want     int
	}{
		{"see D333 and D33.", "D33", 1},
		{"see D333 only", "D33", 0},
		{"xD33 and D33x", "D33", 0},
		{"D33-D40 range", "D33", 1},
		{"(D33)", "D33", 1},
		{"M3.ABC-1 is a task", "M3", 0},
		{"M3.ABC-1 is a task", "M3.ABC-1", 1},
		{"GF-12 and GF-123", "GF-12", 1},
		{"NFR4 not FR4", "FR4", 1},
		{"NFR4 only", "FR4", 0},
	}
	for _, c := range cases {
		if got := len(m.Find(c.line, c.id)); got != c.want {
			t.Errorf("Find(%q, %q) = %d, want %d", c.line, c.id, got, c.want)
		}
	}
}

func TestFindAll(t *testing.T) {
	m := builtin(t)
	var ids []string
	for _, x := range m.FindAll("D1, V22, FP-EXEC-3, M10.DISK-1, RT-Q4, Q-AB-2, C07, T12, PQ-X-1 and C123") {
		ids = append(ids, x.ID+"/"+x.Pattern)
	}
	want := []string{"D1/D", "V22/V", "FP-EXEC-3/FP", "M10.DISK-1/M", "RT-Q4/RT-Q", "Q-AB-2/Q", "C07/C", "T12/T", "PQ-X-1/PQ"}
	if !slices.Equal(ids, want) {
		t.Fatalf("got %v\nwant %v", ids, want)
	}
}

func TestClassify(t *testing.T) {
	m := builtin(t)
	if p, ok := m.Classify("GF-7"); !ok || p != "GF" {
		t.Errorf("GF-7: %q %v", p, ok)
	}
	for _, s := range []string{"5.4", "D", "d333", "TypeScript UI"} {
		if _, ok := m.Classify(s); ok {
			t.Errorf("%q should not classify", s)
		}
	}
}

func TestNewRejectsBadPatterns(t *testing.T) {
	for _, ps := range [][]Pattern{
		nil,
		{{Name: "", Regex: "X"}},
		{{Name: "A", Regex: "("}},
		{{Name: "A", Regex: "A\\d"}, {Name: "A", Regex: "B\\d"}},
	} {
		if _, err := New(ps); err == nil {
			t.Errorf("New(%v) should fail", ps)
		}
	}
}

func TestCompare(t *testing.T) {
	ids := []string{"D10", "D2", "D1", "M1.X-10", "M1.X-2"}
	slices.SortFunc(ids, Compare)
	want := []string{"D1", "D2", "D10", "M1.X-2", "M1.X-10"}
	if !slices.Equal(ids, want) {
		t.Fatalf("got %v", ids)
	}
}
