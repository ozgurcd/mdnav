package hook

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unicode/utf8"
)

// maxContent is the most of any file content a recording keeps.
const maxContent = 4 << 10

// contentFields name the members that carry file content. Commands and
// patch text are kept whole so a recording replays as it ran.
var contentFields = map[string]bool{
	"content": true, "old_string": true, "new_string": true,
	"oldString": true, "newString": true, "new_source": true,
}

// Record writes raw to dir/<agent>-<time>.json (dir 0700, created when
// missing and refused when open to other users; file 0600) with
// each file content cut to maxContent bytes, and returns the file's path.
// A cut recording gains "mdnav_record_truncated": true. Input that is not
// JSON is written as it came.
func Record(dir, agent string, raw []byte, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if fi, err := os.Stat(dir); err != nil {
		return "", err
	} else if fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is open to other users (mode %v); chmod 700 it or name a new directory", dir, fi.Mode().Perm())
	}
	body := raw
	if cut, ok := truncateContent(raw); ok {
		body = cut
	}
	base := agent + "-" + now.UTC().Format("20060102T150405.000000000Z")
	for i := 0; ; i++ {
		name := base + ".json"
		if i > 0 {
			name = base + "-" + strconv.Itoa(i) + ".json"
		}
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) && i < 100 {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(body)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return p, werr
	}
}

// truncateContent re-encodes raw token by token, cutting content members.
// ok is false when raw is not one JSON value.
func truncateContent(raw []byte) (out []byte, ok bool) {
	dec := jsontext.NewDecoder(bytes.NewReader(raw))
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf, jsontext.Multiline(true))
	names := map[int]string{} // the last member name at each depth
	cut := false
	for {
		tok, err := dec.ReadToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}
		depth := dec.StackDepth()
		kind, n := dec.StackIndex(depth)
		if tok.Kind() == '"' && kind == '{' && n%2 == 1 {
			names[depth] = tok.String() // a member name
		} else if tok.Kind() == '"' && kind == '{' && contentFields[names[depth]] && len(tok.String()) > maxContent {
			tok = jsontext.String(cutUTF8(tok.String(), maxContent))
			cut = true
		}
		if cut && depth == 0 && tok.Kind() == '}' {
			if enc.WriteToken(jsontext.String("mdnav_record_truncated")) != nil || enc.WriteToken(jsontext.True) != nil {
				return nil, false
			}
		}
		if enc.WriteToken(tok) != nil {
			return nil, false
		}
	}
	return buf.Bytes(), true
}

func cutUTF8(s string, n int) string {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
