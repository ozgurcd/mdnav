// Package shell splits a POSIX shell command line into simple commands, with
// enough fidelity to judge what each command reads and writes: quotes,
// escapes, comments, pipes and lists, redirections, here-documents (whose
// bodies are data and are skipped) and command or process substitutions
// (whose commands are returned too). It never expands anything and never runs
// anything.
package shell

import (
	"errors"
	"strings"
)

// Command is one simple command.
type Command struct {
	Words  []string // after quote removal; substitutions keep their raw text
	Redirs []Redir
	Piped  bool // standard input comes from a pipe
}

// Redir is one redirection with a file target. Descriptor duplications
// (2>&1, >&-) are dropped; here-documents keep their delimiter as Target.
type Redir struct {
	Op     string // < > >> >| <> &> &>> >& <& << <<- <<<
	Target string
}

// Script is a parsed command line.
type Script struct {
	Commands []Command
	Comments []string // text after each unquoted "#", without the "#"
}

// ErrSyntax reports input the lexer cannot split, such as an unterminated quote.
var ErrSyntax = errors.New("shell syntax")

const maxDepth = 8

// Parse splits src into simple commands.
func Parse(src string) (Script, error) {
	var sc Script
	err := parse(src, 0, &sc)
	return sc, err
}

func parse(src string, depth int, sc *Script) error {
	if depth > maxDepth {
		return errors.Join(ErrSyntax, errors.New("substitutions nest too deep"))
	}
	l := &lexer{s: src, sc: sc, depth: depth}
	return l.run()
}

type heredoc struct {
	delim     string
	stripTabs bool
}

type lexer struct {
	s       string
	i       int
	depth   int
	sc      *Script
	cur     Command
	word    strings.Builder
	inWord  bool
	redir   string // operator waiting for its target word
	pending []heredoc
}

func (l *lexer) run() error {
	for l.i < len(l.s) {
		c := l.s[l.i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			l.endWord()
			l.i++
		case c == '\n':
			l.endWord()
			l.endCommand(false)
			l.i++
			l.skipHeredocs()
		case c == '#' && !l.inWord:
			end := strings.IndexByte(l.s[l.i:], '\n')
			if end < 0 {
				end = len(l.s) - l.i
			}
			l.sc.Comments = append(l.sc.Comments, l.s[l.i+1:l.i+end])
			l.i += end
		case c == '\\':
			if l.i+1 < len(l.s) {
				if l.s[l.i+1] != '\n' {
					l.add(l.s[l.i+1 : l.i+2])
				}
				l.i += 2
			} else {
				l.i++
			}
		case c == '\'':
			end := strings.IndexByte(l.s[l.i+1:], '\'')
			if end < 0 {
				return errors.Join(ErrSyntax, errors.New("unterminated single quote"))
			}
			l.add(l.s[l.i+1 : l.i+1+end])
			l.i += end + 2
		case c == '"':
			if err := l.double(); err != nil {
				return err
			}
		case c == '`':
			if err := l.backtick(); err != nil {
				return err
			}
		case c == '$':
			if err := l.dollar(); err != nil {
				return err
			}
		case c == '|':
			l.endWord()
			switch {
			case l.peek(1) == '|':
				l.i += 2
				l.endCommand(false)
			case l.peek(1) == '&':
				l.i += 2
				l.endCommand(true)
			default:
				l.i++
				l.endCommand(true)
			}
		case c == '&':
			l.endWord()
			switch l.peek(1) {
			case '&':
				l.i += 2
				l.endCommand(false)
			case '>':
				l.redirOp()
			default:
				l.i++
				l.endCommand(false)
			}
		case c == ';':
			l.endWord()
			l.i++
			l.endCommand(false)
		case c == '(' || c == ')':
			l.endWord()
			l.i++
			if len(l.cur.Words) > 0 || len(l.cur.Redirs) > 0 {
				l.endCommand(false)
			}
		case c == '<' || c == '>':
			if l.peek(1) == '(' { // process substitution
				l.i++
				if err := l.substitution('(', ')', l.i-1); err != nil {
					return err
				}
				continue
			}
			if l.inWord && isDigits(l.word.String()) { // 2>file: the digits name a descriptor
				l.word.Reset()
				l.inWord = false
			}
			l.endWord()
			l.redirOp()
		default:
			l.add(l.s[l.i : l.i+1])
			l.i++
		}
	}
	l.endWord()
	l.endCommand(false)
	return nil
}

func (l *lexer) peek(n int) byte {
	if l.i+n < len(l.s) {
		return l.s[l.i+n]
	}
	return 0
}

func (l *lexer) add(s string) {
	l.word.WriteString(s)
	l.inWord = true
}

func (l *lexer) redirOp() {
	for _, op := range []string{"&>>", "&>", "<<<", "<<-", "<<", "<>", "<&", "<", ">>", ">|", ">&", ">"} {
		if strings.HasPrefix(l.s[l.i:], op) {
			l.redir = op
			l.i += len(op)
			return
		}
	}
}

func (l *lexer) endWord() {
	if !l.inWord {
		return
	}
	w := l.word.String()
	l.word.Reset()
	l.inWord = false
	if op := l.redir; op != "" {
		l.redir = ""
		switch {
		case op == "<<" || op == "<<-":
			l.pending = append(l.pending, heredoc{delim: w, stripTabs: op == "<<-"})
		case (op == ">&" || op == "<&") && (isDigits(w) || w == "-"):
			return // descriptor duplication, not a file
		}
		l.cur.Redirs = append(l.cur.Redirs, Redir{Op: op, Target: w})
		return
	}
	l.cur.Words = append(l.cur.Words, w)
}

func (l *lexer) endCommand(nextPiped bool) {
	if len(l.cur.Words) > 0 || len(l.cur.Redirs) > 0 {
		l.sc.Commands = append(l.sc.Commands, l.cur)
	}
	l.cur = Command{Piped: nextPiped}
	l.redir = ""
}

// skipHeredocs consumes the bodies of the here-documents opened on the line
// that just ended: they are data, not commands.
func (l *lexer) skipHeredocs() {
	for _, h := range l.pending {
		for l.i < len(l.s) {
			end := strings.IndexByte(l.s[l.i:], '\n')
			line := l.s[l.i:]
			if end >= 0 {
				line = l.s[l.i : l.i+end]
				l.i += end + 1
			} else {
				l.i = len(l.s)
			}
			if h.stripTabs {
				line = strings.TrimLeft(line, "\t")
			}
			if strings.TrimRight(line, "\r") == h.delim {
				break
			}
		}
	}
	l.pending = nil
}

func (l *lexer) double() error {
	l.i++
	l.inWord = true
	for l.i < len(l.s) {
		c := l.s[l.i]
		switch {
		case c == '"':
			l.i++
			return nil
		case c == '\\' && l.i+1 < len(l.s) && strings.IndexByte("$`\"\\\n", l.s[l.i+1]) >= 0:
			if l.s[l.i+1] != '\n' {
				l.add(l.s[l.i+1 : l.i+2])
			}
			l.i += 2
		case c == '$':
			if err := l.dollar(); err != nil {
				return err
			}
		case c == '`':
			if err := l.backtick(); err != nil {
				return err
			}
		default:
			l.add(l.s[l.i : l.i+1])
			l.i++
		}
	}
	return errors.Join(ErrSyntax, errors.New("unterminated double quote"))
}

func (l *lexer) dollar() error {
	start := l.i
	switch l.peek(1) {
	case '(':
		if l.peek(2) == '(' { // arithmetic: no commands inside
			end, err := closing(l.s, l.i+1, '(', ')')
			if err != nil {
				return err
			}
			l.add(l.s[start : end+1])
			l.i = end + 1
			return nil
		}
		l.i++
		return l.substitution('(', ')', start)
	case '{':
		end, err := closing(l.s, l.i+1, '{', '}')
		if err != nil {
			return err
		}
		l.add(l.s[start : end+1])
		l.i = end + 1
	case '\'':
		j := l.i + 2
		for j < len(l.s) && l.s[j] != '\'' {
			if l.s[j] == '\\' {
				j++
			}
			j++
		}
		if j >= len(l.s) {
			return errors.Join(ErrSyntax, errors.New("unterminated $'...'"))
		}
		l.add(l.s[l.i+2 : j])
		l.i = j + 1
	default:
		l.add("$")
		l.i++
	}
	return nil
}

// substitution parses the commands of $(...), <(...) or >(...); l.i is at the
// opening parenthesis and start at the first character of the construct.
func (l *lexer) substitution(open, close byte, start int) error {
	end, err := closing(l.s, l.i, open, close)
	if err != nil {
		return err
	}
	if err := parse(l.s[l.i+1:end], l.depth+1, l.sc); err != nil {
		return err
	}
	l.add(l.s[start : end+1])
	l.i = end + 1
	return nil
}

func (l *lexer) backtick() error {
	var inner strings.Builder
	j := l.i + 1
	for ; j < len(l.s) && l.s[j] != '`'; j++ {
		if l.s[j] == '\\' && j+1 < len(l.s) {
			j++
		}
		inner.WriteByte(l.s[j])
	}
	if j >= len(l.s) {
		return errors.Join(ErrSyntax, errors.New("unterminated backquote"))
	}
	if err := parse(inner.String(), l.depth+1, l.sc); err != nil {
		return err
	}
	l.add(l.s[l.i : j+1])
	l.i = j + 1
	return nil
}

// closing returns the index of the bracket that closes the one at s[i],
// skipping quoted text.
func closing(s string, i int, open, close byte) (int, error) {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '\'':
			k := strings.IndexByte(s[j+1:], '\'')
			if k < 0 {
				return 0, errors.Join(ErrSyntax, errors.New("unterminated single quote"))
			}
			j += k + 1
		case '"':
			for j++; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' {
					j++
				}
			}
		case open:
			depth++
		case close:
			if depth--; depth == 0 {
				return j, nil
			}
		}
	}
	return 0, errors.Join(ErrSyntax, errors.New("unbalanced "+string(open)))
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
