// Package markdown is a small, line-based Markdown parser. It understands the
// block shapes that design documents use: ATX headings, pipe tables, nested
// list items, paragraphs, fenced code blocks and blockquotes. It is not a
// CommonMark implementation and renders nothing.
//
// All line numbers in this package are 1-based and ranges are inclusive.
package markdown

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
)

// Kind is the type of a leaf block.
type Kind int

// Block kinds.
const (
	KindParagraph Kind = iota
	KindListItem
	KindTableRow
	KindCode
	KindQuote
	KindHeading
)

// String returns a short human-readable name for the kind.
func (k Kind) String() string {
	switch k {
	case KindParagraph:
		return "paragraph"
	case KindListItem:
		return "list item"
	case KindTableRow:
		return "table row"
	case KindCode:
		return "code block"
	case KindQuote:
		return "blockquote"
	case KindHeading:
		return "heading"
	}
	return "block"
}

// Heading is an ATX heading and the section it opens.
type Heading struct {
	Level  int    // 1..6
	Text   string // heading text without the leading #s
	Number string // normalised section number ("5.4"), or "" if none
	Line   int    // line of the heading itself
	End    int    // last line of the section
	Parent int    // index of the enclosing heading in Doc.Headings, or -1
}

// Table is a pipe table.
type Table struct {
	Header      int // header row line
	Sep         int // separator row line
	Start, End  int // first (header) and last row line
	HeaderCells []string
}

// Block is a leaf block. List items may contain other blocks (nested items,
// code) and therefore overlap them; every other kind is disjoint.
type Block struct {
	Kind       Kind
	Start, End int
	Table      int      // index into Doc.Tables for table rows, else -1
	IsHeader   bool     // true for a table's header row
	Cells      []string // cells of a table row
	Heading    int      // index into Doc.Headings for KindHeading, else -1
}

// Doc is a parsed Markdown file.
type Doc struct {
	Path     string
	Lines    []string // without line terminators
	Headings []Heading
	Tables   []Table
	Blocks   []Block // sorted by Start, outer blocks before inner ones

	code      []bool      // code[i]: line i+1 is inside a fenced code block (fences included)
	fences    map[int]int // opening fence index -> closing fence index (0-based)
	openFence bool        // the last fenced code block has no closing fence
	smallest  []int       // smallest[i]: index of the smallest block containing line i+1, or -1
}

// NumLines returns the number of lines in the document.
func (d *Doc) NumLines() int { return len(d.Lines) }

// Line returns the text of line n (1-based), or "" when out of range.
func (d *Doc) Line(n int) string {
	if n < 1 || n > len(d.Lines) {
		return ""
	}
	return d.Lines[n-1]
}

// InCode reports whether line n is part of a fenced code block, fences included.
func (d *Doc) InCode(n int) bool {
	return n >= 1 && n <= len(d.code) && d.code[n-1]
}

// Smallest returns the smallest block that contains line n.
func (d *Doc) Smallest(n int) (Block, bool) {
	if n < 1 || n > len(d.smallest) || d.smallest[n-1] < 0 {
		return Block{}, false
	}
	return d.Blocks[d.smallest[n-1]], true
}

// HeadingAt returns the index of the innermost heading whose section contains
// line n, or -1 when the line comes before the first heading.
func (d *Doc) HeadingAt(n int) int {
	i := sort.Search(len(d.Headings), func(i int) bool { return d.Headings[i].Line > n })
	return i - 1
}

// HeadingPath returns the heading indexes from the outermost section down to h.
func (d *Doc) HeadingPath(h int) []int {
	var path []int
	for h >= 0 {
		path = append(path, h)
		h = d.Headings[h].Parent
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

var (
	headingRe   = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$`)
	closingRe   = regexp.MustCompile(`(?:^|[ \t]+)#+$`)
	fenceRe     = regexp.MustCompile("^([ \t]*)(`{3,}|~{3,})(.*)$")
	separatorRe = regexp.MustCompile(`^[ \t]*\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$`)
	markerRe    = regexp.MustCompile(`^([ \t]*)([-*+]|[0-9]{1,9}[.)])(?:([ \t]+)|$)`)
	quoteRe     = regexp.MustCompile(`^ {0,3}>`)
	numberRe    = regexp.MustCompile(`^([0-9]+[a-z]?(?:\.[0-9]+[a-z]?)*)\.?(?:[ \t]|$)`)
)

// SplitLines splits data into lines, accepting LF and CRLF terminators.
func SplitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	data = bytes.TrimSuffix(data, []byte("\n"))
	parts := strings.Split(string(data), "\n")
	for i, p := range parts {
		parts[i] = strings.TrimSuffix(p, "\r")
	}
	return parts
}

// Parse parses data as a Markdown document named path.
func Parse(path string, data []byte) *Doc {
	d := &Doc{Path: path, Lines: SplitLines(data)}
	n := len(d.Lines)
	d.code = make([]bool, n)
	d.fences = make(map[int]int)
	owned := make([]bool, n) // lines claimed by a heading, table, code or quote

	d.parseFences(owned)
	d.parseHeadings(owned)
	d.parseTables(owned)
	d.parseQuotes(owned)
	inItem := d.parseListItems(owned)
	d.parseParagraphs(owned, inItem)

	sort.SliceStable(d.Blocks, func(i, j int) bool {
		a, b := d.Blocks[i], d.Blocks[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.End > b.End
	})
	d.indexSmallest()
	return d
}

func (d *Doc) parseFences(owned []bool) {
	for i := 0; i < len(d.Lines); i++ {
		m := fenceRe.FindStringSubmatch(d.Lines[i])
		if m == nil || (m[2][0] == '`' && strings.Contains(m[3], "`")) {
			continue
		}
		char, size := m[2][0], len(m[2])
		end := len(d.Lines) - 1 // an unclosed fence runs to the end of the file
		closed := false
		for j := i + 1; j < len(d.Lines); j++ {
			if isClosingFence(d.Lines[j], char, size) {
				end, closed = j, true
				break
			}
		}
		d.openFence = !closed
		for k := i; k <= end; k++ {
			d.code[k] = true
			owned[k] = true
		}
		d.fences[i] = end
		d.Blocks = append(d.Blocks, Block{Kind: KindCode, Start: i + 1, End: end + 1, Table: -1, Heading: -1})
		i = end
	}
}

func isClosingFence(line string, char byte, size int) bool {
	t := strings.TrimSpace(line)
	if len(t) < size {
		return false
	}
	for k := 0; k < len(t); k++ {
		if t[k] != char {
			return false
		}
	}
	return true
}

func (d *Doc) parseHeadings(owned []bool) {
	for i, line := range d.Lines {
		if d.code[i] {
			continue
		}
		m := headingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		text := strings.TrimSpace(closingRe.ReplaceAllString(m[2], ""))
		d.Headings = append(d.Headings, Heading{
			Level:  len(m[1]),
			Text:   text,
			Number: SectionNumber(text),
			Line:   i + 1,
			Parent: -1,
		})
		owned[i] = true
		d.Blocks = append(d.Blocks, Block{Kind: KindHeading, Start: i + 1, End: i + 1, Table: -1, Heading: len(d.Headings) - 1})
	}
	for i := range d.Headings {
		h := &d.Headings[i]
		h.End = len(d.Lines)
		for j := i + 1; j < len(d.Headings); j++ {
			if d.Headings[j].Level <= h.Level {
				h.End = d.Headings[j].Line - 1
				break
			}
		}
		for j := i - 1; j >= 0; j-- {
			if d.Headings[j].Level < h.Level {
				h.Parent = j
				break
			}
		}
	}
}

func (d *Doc) parseTables(owned []bool) {
	for i := 0; i+1 < len(d.Lines); i++ {
		if owned[i] || owned[i+1] || !hasUnescapedPipe(d.Lines[i]) ||
			!strings.Contains(d.Lines[i+1], "|") || !separatorRe.MatchString(d.Lines[i+1]) {
			continue
		}
		t := Table{Header: i + 1, Sep: i + 2, Start: i + 1, End: i + 2, HeaderCells: SplitCells(d.Lines[i])}
		ti := len(d.Tables)
		d.Blocks = append(d.Blocks, Block{Kind: KindTableRow, Start: i + 1, End: i + 1, Table: ti, IsHeader: true, Cells: t.HeaderCells, Heading: -1})
		owned[i], owned[i+1] = true, true
		j := i + 2
		for ; j < len(d.Lines); j++ {
			line := d.Lines[j]
			if owned[j] || isBlank(line) || !hasUnescapedPipe(line) {
				break
			}
			owned[j] = true
			d.Blocks = append(d.Blocks, Block{Kind: KindTableRow, Start: j + 1, End: j + 1, Table: ti, Cells: SplitCells(line), Heading: -1})
		}
		t.End = j
		d.Tables = append(d.Tables, t)
		i = j - 1
	}
}

func (d *Doc) parseQuotes(owned []bool) {
	for i := 0; i < len(d.Lines); i++ {
		if owned[i] || !quoteRe.MatchString(d.Lines[i]) {
			continue
		}
		j := i
		for j+1 < len(d.Lines) && !owned[j+1] && quoteRe.MatchString(d.Lines[j+1]) {
			j++
		}
		for k := i; k <= j; k++ {
			owned[k] = true
		}
		d.Blocks = append(d.Blocks, Block{Kind: KindQuote, Start: i + 1, End: j + 1, Table: -1, Heading: -1})
		i = j
	}
}

// parseListItems adds one block per list item. An item runs over its
// continuation lines, nested items and indented code. It returns which lines
// lie inside some item.
func (d *Doc) parseListItems(owned []bool) []bool {
	inItem := make([]bool, len(d.Lines))
	for i := range d.Lines {
		if owned[i] {
			continue
		}
		indent, content, ok := listMarker(d.Lines[i])
		if !ok {
			continue
		}
		end := d.itemEnd(i, indent, content, owned)
		for k := i; k <= end; k++ {
			inItem[k] = true
		}
		d.Blocks = append(d.Blocks, Block{Kind: KindListItem, Start: i + 1, End: end + 1, Table: -1, Heading: -1})
	}
	return inItem
}

func (d *Doc) itemEnd(i, indent, content int, owned []bool) int {
	end := i
	for j := i + 1; j < len(d.Lines); j++ {
		line := d.Lines[j]
		switch {
		case d.code[j]:
			// A fence belongs to the item only when it opens at the item's
			// content indentation.
			close, open := d.fences[j]
			if !open || indentWidth(line) < content {
				return end
			}
			end, j = close, close
		case isBlank(line):
			k := j + 1
			for k < len(d.Lines) && isBlank(d.Lines[k]) {
				k++
			}
			if k == len(d.Lines) || !d.continues(k, indent, content) {
				return end
			}
			j = k - 1
		case d.continues(j, indent, content):
			end = j
		case !owned[j] && !isBlank(d.Lines[j-1]) && !isMarkerLine(line):
			end = j // lazy continuation of the item's paragraph
		default:
			return end
		}
	}
	return end
}

// continues reports whether line j, being non-blank, is indented enough to
// belong to an item with the given marker indent and content offset.
func (d *Doc) continues(j, indent, content int) bool {
	w := indentWidth(d.Lines[j])
	if w >= content {
		return true
	}
	_, _, ok := listMarker(d.Lines[j])
	return ok && w > indent && !d.code[j]
}

func isMarkerLine(line string) bool {
	_, _, ok := listMarker(line)
	return ok
}

// listMarker returns the marker's indent width and the item's content offset.
func listMarker(line string) (indent, content int, ok bool) {
	m := markerRe.FindStringSubmatchIndex(line)
	if m == nil {
		return 0, 0, false
	}
	indent = indentWidth(line[:m[3]])
	content = indent + (m[5] - m[4]) + 1
	if m[6] >= 0 {
		if spaces := m[7] - m[6]; spaces <= 4 {
			content = indent + (m[5] - m[4]) + spaces
		}
	}
	return indent, content, true
}

func (d *Doc) parseParagraphs(owned, inItem []bool) {
	free := func(i int) bool { return !owned[i] && !inItem[i] && !isBlank(d.Lines[i]) }
	for i := 0; i < len(d.Lines); i++ {
		if !free(i) {
			continue
		}
		j := i
		for j+1 < len(d.Lines) && free(j+1) {
			j++
		}
		d.Blocks = append(d.Blocks, Block{Kind: KindParagraph, Start: i + 1, End: j + 1, Table: -1, Heading: -1})
		i = j
	}
}

func (d *Doc) indexSmallest() {
	d.smallest = make([]int, len(d.Lines))
	for i := range d.smallest {
		d.smallest[i] = -1
	}
	order := make([]int, len(d.Blocks))
	for i := range order {
		order[i] = i
	}
	// Largest first, so smaller blocks overwrite the lines they share.
	sort.SliceStable(order, func(a, b int) bool {
		x, y := d.Blocks[order[a]], d.Blocks[order[b]]
		return x.End-x.Start > y.End-y.Start
	})
	for _, bi := range order {
		b := d.Blocks[bi]
		for l := b.Start; l <= b.End; l++ {
			d.smallest[l-1] = bi
		}
	}
}

// SectionNumber extracts the normalised section number from heading text:
// "5.4 Reconcile loop" gives "5.4" and "05. Control plane" gives "5".
func SectionNumber(text string) string {
	m := numberRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return normaliseNumber(m[1])
}

// ParseSectionQuery recognises "§5.4", "§ 5.4", "5.4" and "05." and returns
// the normalised number.
func ParseSectionQuery(q string) (string, bool) {
	q = strings.TrimSpace(q)
	q = strings.TrimSpace(strings.TrimPrefix(q, "§"))
	q = strings.TrimSuffix(q, ".")
	if q == "" {
		return "", false
	}
	m := numberRe.FindStringSubmatch(q)
	if m == nil || m[1] != q {
		return "", false
	}
	return normaliseNumber(q), true
}

func normaliseNumber(s string) string {
	parts := strings.Split(s, ".")
	for i, p := range parts {
		t := strings.TrimLeft(p, "0")
		if t == "" || t[0] < '0' || t[0] > '9' {
			t = "0" + t
		}
		parts[i] = t
	}
	return strings.Join(parts, ".")
}

// SplitCells splits a table row into trimmed cells. Escaped pipes (\|) stay
// inside their cell and are unescaped.
func SplitCells(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case s[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// StrikeSpans returns the byte ranges [start, end) of ~~struck~~ text in line,
// including the tildes.
func StrikeSpans(line string) [][2]int {
	var spans [][2]int
	for off := 0; ; {
		a := strings.Index(line[off:], "~~")
		if a < 0 {
			return spans
		}
		a += off
		b := strings.Index(line[a+2:], "~~")
		if b < 0 {
			return spans
		}
		b += a + 2
		spans = append(spans, [2]int{a, b + 2})
		off = b + 2
	}
}

func hasUnescapedPipe(line string) bool {
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if line[i] == '|' {
			return true
		}
	}
	return false
}

func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

// indentWidth returns the width of leading whitespace, expanding tabs to 4.
func indentWidth(line string) int {
	w := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			w++
		case '\t':
			w += 4 - w%4
		default:
			return w
		}
	}
	return w
}

// OpenFence reports whether the document ends inside a fenced code block
// whose closing fence is missing.
func (d *Doc) OpenFence() bool { return d.openFence }

// CellSpans returns the byte range [start, end) of each cell's raw text in a
// table row, between its pipes and with surrounding whitespace included. It
// splits exactly as SplitCells does: an escaped pipe (\|) stays in its cell
// and the optional leading and trailing pipes delimit no cell.
func CellSpans(line string) [][2]int {
	start, end := 0, len(line)
	for start < end && (line[start] == ' ' || line[start] == '\t') {
		start++
	}
	for end > start && (line[end-1] == ' ' || line[end-1] == '\t') {
		end--
	}
	if start < end && line[start] == '|' {
		start++
	}
	if end > start && line[end-1] == '|' && !(end-2 >= start && line[end-2] == '\\') {
		end--
	}
	var spans [][2]int
	cur := start
	for i := start; i < end; i++ {
		switch {
		case line[i] == '\\' && i+1 < end && line[i+1] == '|':
			i++
		case line[i] == '|':
			spans = append(spans, [2]int{cur, i})
			cur = i + 1
		}
	}
	return append(spans, [2]int{cur, end})
}
