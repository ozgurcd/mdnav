// Package idpat recognises document identifiers such as D333, GF-12 or
// M10.DISK-1, always on word boundaries.
package idpat

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Pattern is a named identifier pattern. Regex is an RE2 expression without
// anchors or word boundaries; the matcher adds those.
type Pattern struct {
	Name  string `json:"name"`
	Regex string `json:"regex"`
}

// Builtin returns the default identifier patterns, in display order.
func Builtin() []Pattern {
	return []Pattern{
		{"D", `D\d+`},
		{"V", `V\d+`},
		{"FR", `FR\d+`},
		{"NFR", `NFR\d+`},
		{"GF", `GF-\d+`},
		{"FP", `FP-[A-Z]+-\d+`},
		{"FT", `FT-\d+`},
		{"M", `M\d+(?:\.[A-Z]+-\d+)?`},
		{"M-TASK", `M-[A-Z]+-\d+`},
		{"Q", `Q-[A-Z]+-\d+`},
		{"RT-Q", `RT-Q\d+`},
		{"C", `C\d{2}`},
		{"T", `T\d{2}`},
		{"PQ", `PQ-[A-Z0-9-]+`},
	}
}

// Match is one identifier found in a line.
type Match struct {
	ID         string
	Pattern    string
	Start, End int // byte offsets in the line
}

// Matcher finds identifiers.
type Matcher struct {
	patterns []Pattern
	all      *regexp.Regexp
	exact    []*regexp.Regexp
}

// New compiles the patterns. Names must be unique and non-empty.
func New(patterns []Pattern) (*Matcher, error) {
	if len(patterns) == 0 {
		return nil, fmt.Errorf("no identifier patterns")
	}
	m := &Matcher{patterns: patterns}
	seen := make(map[string]bool)
	var alts []string
	for i, p := range patterns {
		if p.Name == "" || p.Regex == "" {
			return nil, fmt.Errorf("pattern %d: name and regex are required", i+1)
		}
		if seen[p.Name] {
			return nil, fmt.Errorf("pattern %q: duplicate name", p.Name)
		}
		seen[p.Name] = true
		re, err := regexp.Compile(`^(?:` + p.Regex + `)$`)
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", p.Name, err)
		}
		m.exact = append(m.exact, re)
		alts = append(alts, `(?:`+p.Regex+`)`)
	}
	all, err := regexp.Compile(`\b(?:` + strings.Join(alts, "|") + `)\b`)
	if err != nil {
		return nil, err
	}
	m.all = all
	return m, nil
}

// Patterns returns the patterns in display order.
func (m *Matcher) Patterns() []Pattern { return m.patterns }

// Classify reports which pattern s matches in full.
func (m *Matcher) Classify(s string) (string, bool) {
	for i, re := range m.exact {
		if re.MatchString(s) {
			return m.patterns[i].Name, true
		}
	}
	return "", false
}

// FindAll returns every identifier in line, left to right.
func (m *Matcher) FindAll(line string) []Match {
	var out []Match
	// Finding the spans without submatches and classifying each span is much
	// cheaper than asking the combined expression for its groups.
	for _, loc := range m.all.FindAllStringIndex(line, -1) {
		id := line[loc[0]:loc[1]]
		name, _ := m.Classify(id)
		out = append(out, Match{ID: id, Pattern: name, Start: loc[0], End: loc[1]})
	}
	return out
}

// Find returns the byte offsets of every occurrence of id in line. An
// occurrence counts only when the full identifier at that place is id, so D33
// is not found inside D333 and M3 is not found inside M3.ABC-1.
func (m *Matcher) Find(line, id string) [][2]int {
	if !strings.Contains(line, id) {
		return nil
	}
	var out [][2]int
	if _, ok := m.Classify(id); ok {
		for _, x := range m.FindAll(line) {
			if x.ID == id {
				out = append(out, [2]int{x.Start, x.End})
			}
		}
		return out
	}
	// Not a known identifier: fall back to plain word boundaries.
	for off := 0; ; {
		i := strings.Index(line[off:], id)
		if i < 0 {
			return out
		}
		i += off
		j := i + len(id)
		if (i == 0 || !isWord(line[i-1])) && (j == len(line) || !isWord(line[j])) {
			out = append(out, [2]int{i, j})
		}
		off = j
	}
}

// Contains reports whether line mentions id.
func (m *Matcher) Contains(line, id string) bool { return len(m.Find(line, id)) > 0 }

func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// Compare orders identifiers naturally: D2 before D10, M1.X-2 before M1.X-10.
func Compare(a, b string) int {
	for a != "" && b != "" {
		ra, na := splitRun(a)
		rb, nb := splitRun(b)
		if c := compareRun(ra, rb); c != 0 {
			return c
		}
		a, b = na, nb
	}
	return cmp.Compare(len(a), len(b))
}

func splitRun(s string) (run, rest string) {
	digit := s[0] >= '0' && s[0] <= '9'
	i := 1
	for i < len(s) && (s[i] >= '0' && s[i] <= '9') == digit {
		i++
	}
	return s[:i], s[i:]
}

func compareRun(a, b string) int {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	if ea == nil && eb == nil {
		if c := cmp.Compare(na, nb); c != 0 {
			return c
		}
		return cmp.Compare(len(a), len(b))
	}
	return strings.Compare(a, b)
}
