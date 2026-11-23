package edit

import "testing"

func TestSplice(t *testing.T) {
	cases := []struct {
		name, in string
		from, to int
		lines    []string
		want     string
	}{
		{"replace middle", "a\nb\nc\n", 2, 2, []string{"B1", "B2"}, "a\nB1\nB2\nc\n"},
		{"replace last, final newline kept", "a\nb\n", 2, 2, []string{"B"}, "a\nB\n"},
		{"replace last, no final newline kept", "a\nb", 2, 2, []string{"B"}, "a\nB"},
		{"crlf preserved", "a\r\nb\r\nc\r\n", 2, 2, []string{"x", "y"}, "a\r\nx\r\ny\r\nc\r\n"},
		{"delete middle", "a\nb\nc\n", 2, 2, nil, "a\nc\n"},
		{"delete last with final newline", "a\nb\n", 2, 2, nil, "a\n"},
		{"delete last without final newline", "a\nb", 2, 2, nil, "a"},
		{"delete last without final newline crlf", "a\r\nb", 2, 2, nil, "a"},
		{"delete only line", "a\n", 1, 1, nil, ""},
		{"insert before first", "a\n", 1, 0, []string{"z"}, "z\na\n"},
		{"insert in middle", "a\nc\n", 2, 1, []string{"b"}, "a\nb\nc\n"},
		{"append with final newline", "a\n", 2, 1, []string{"b"}, "a\nb\n"},
		{"append without final newline", "a", 2, 1, []string{"b"}, "a\nb"},
		{"append to empty file", "", 1, 0, []string{"a"}, "a\n"},
		{"mixed endings outside range untouched", "a\r\nb\nc\r\n", 2, 2, []string{"B"}, "a\r\nB\r\nc\r\n"},
	}
	for _, c := range cases {
		got := string(newText([]byte(c.in)).splice(c.from, c.to, c.lines))
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
