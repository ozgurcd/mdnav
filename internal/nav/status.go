package nav

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ozgurcd/mdnav/internal/markdown"
)

// Status verdicts. They are hints drawn from the text, never a ruling.
const (
	VerdictActive   = "active (no supersession text found)"
	VerdictPossibly = "possibly superseded"
	VerdictNotFound = "not found (no line mentions it)"
)

// StatusHit is one line that suggests the identifier may be superseded.
type StatusHit struct {
	Path        string   `json:"path"`
	Line        int      `json:"line"`
	Where       string   `json:"where,omitzero"`
	Hash        string   `json:"hash"` // BlockHash of the block an edit at Line would target
	Reason      string   `json:"reason"`
	Superseders []string `json:"superseders,omitzero"` // ids on the same line that appear to supersede it
	Text        string   `json:"text"`
}

// StatusResult is a supersession hint for one identifier.
type StatusResult struct {
	ID       string      `json:"id"`
	Verdict  string      `json:"verdict"`
	Summary  string      `json:"summary"`
	Defined  []Ref       `json:"defined,omitzero"`
	Mentions int         `json:"mentions"`
	Hits     []StatusHit `json:"hits,omitzero"`
	More     int         `json:"more,omitzero"`
}

var (
	// Active forms: the line's subject supersedes what follows.
	activeRe = regexp.MustCompile(`(?i)\bsupersed(?:e|es|ing)\b`)
	// Passive forms: something on the line is superseded.
	passiveRe = regexp.MustCompile(`(?i)\b(?:superseded|supersession)\b`)
	// "superseded (D1, D2)", "superseded by D3", "Superseded in part (D4):".
	// The ids in the clause are the superseders.
	byClauseRe = regexp.MustCompile(`(?i)\bsuperseded(?:\s+in\s+part)?(?:\s+by)?\s*(?:\(([^)]*)\)|((?:[A-Z][A-Z0-9.-]*\d[\s,]*(?:and\s+)?)+))`)
)

// Status reports whether the text suggests id is superseded. A line counts
// when id is struck through, when id's own row says "superseded", when id
// follows "supersedes", or when id shares a line with "superseded" outside a
// "superseded (by ...)" clause that names it as the superseder.
func (n *Navigator) Status(ctx context.Context, id string, o Options) (StatusResult, error) {
	res := StatusResult{ID: id}
	var hits []StatusHit
	for _, d := range n.docs {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		for ln := 1; ln <= d.NumLines(); ln++ {
			if d.InCode(ln) {
				continue
			}
			line := d.Line(ln)
			occ := n.ids.Find(line, id)
			if len(occ) == 0 {
				continue
			}
			res.Mentions++
			if isDefRow(d, ln, id) {
				res.Defined = append(res.Defined, Ref{Path: d.Path, Line: ln, Where: headingTrail(d, ln), Text: snippet(line, occ[0][0], occ[0][1], false)})
			}
			if h, ok := n.statusHit(d, ln, id, occ); ok {
				h.Text = snippet(line, occ[0][0], occ[0][1], o.Full)
				hits = append(hits, h)
			}
		}
	}
	switch {
	case len(hits) > 0:
		res.Verdict = VerdictPossibly
		var locs []string
		for _, h := range hits {
			locs = append(locs, fmt.Sprintf("%s:%d", h.Path, h.Line))
		}
		res.Summary = VerdictPossibly + ": see lines " + strings.Join(locs, ", ")
	case res.Mentions == 0:
		res.Verdict, res.Summary = VerdictNotFound, VerdictNotFound
	default:
		res.Verdict, res.Summary = VerdictActive, VerdictActive
	}
	if lim := o.limit(DefaultListLimit); lim >= 0 && len(hits) > lim {
		res.More = len(hits) - lim
		hits = hits[:lim]
	}
	res.Hits = hits
	return res, nil
}

func (n *Navigator) statusHit(d *markdown.Doc, ln int, id string, occ [][2]int) (StatusHit, bool) {
	line := d.Line(ln)
	h := StatusHit{Path: d.Path, Line: ln, Where: n.blockWhere(d, ln), Hash: spanHash(d, ln)}

	// "superseded (by) ..." clauses name superseders; mentions of id inside
	// them say id supersedes something else.
	var clauses [][2]int
	var superseders []string
	for _, m := range byClauseRe.FindAllStringSubmatchIndex(line, -1) {
		clauses = append(clauses, [2]int{m[0], m[1]})
		for _, x := range n.ids.FindAll(line[m[0]:m[1]]) {
			superseders = appendUnique(superseders, x.ID, id)
		}
	}
	inside := func(p [2]int, spans [][2]int) bool {
		for _, s := range spans {
			if p[0] >= s[0] && p[1] <= s[1] {
				return true
			}
		}
		return false
	}
	var free [][2]int // mentions outside superseder clauses
	struck := false
	strikes := markdown.StrikeSpans(line)
	for _, p := range occ {
		if inside(p, strikes) {
			struck = true
		}
		if !inside(p, clauses) {
			free = append(free, p)
		}
	}
	active := activeRe.FindAllStringIndex(line, -1)
	passive := passiveRe.FindStringIndex(line) != nil

	switch {
	case struck:
		h.Reason = "struck through (~~...~~)"
	case len(free) == 0:
		return h, false
	case isDefRow(d, ln, id) && passive:
		h.Reason = "its own row mentions supersession"
	case len(active) > 0 && active[0][0] < free[len(free)-1][0]:
		word := line[active[0][0]:active[0][1]]
		h.Reason = fmt.Sprintf("named after %q", word)
		// The subject of "supersedes" is the row's id, or else the nearest id
		// before the word.
		b, _ := d.Smallest(ln)
		if rid := n.rowID(b); rid != "" {
			superseders = appendUnique(superseders, rid, id)
		} else {
			var last string
			for _, x := range n.ids.FindAll(line[:active[0][0]]) {
				last = x.ID
			}
			if last != "" {
				superseders = appendUnique(superseders, last, id)
			}
		}
	case passive:
		h.Reason = `on the same line as "superseded"`
	default:
		return h, false
	}
	h.Superseders = superseders
	return h, true
}

func appendUnique(list []string, v, skip string) []string {
	if v == skip || slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}
