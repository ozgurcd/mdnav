package hook

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
// each file content cut to maxContent bytes, environment values dropped and
// token-like strings redacted, and returns the file's path.
func Record(dir, agent string, raw []byte, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if fi, err := os.Stat(dir); err != nil {
		return "", err
	} else if fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is open to other users (mode %v); chmod 700 it or name a new directory", dir, fi.Mode().Perm())
	}
	body := []byte(redact(string(raw))) // input that is not JSON
	if clean, ok := sanitize(raw); ok {
		body = clean
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

// envFields name members whose values are environment variables: their
// names are kept, their values never recorded.
var envFields = map[string]bool{"env": true, "environment": true}

// tokenLike matches strings shaped like credentials: bearer and basic
// tokens, well-known key prefixes, JWTs, and NAME=value where NAME says
// token, secret, password or key.
var tokenLike = regexp.MustCompile(`(?i)\b(?:bearer|basic|token)\s+[A-Za-z0-9._~+/=-]{16,}` +
	`|\b(?:sk|pk|rk)-[A-Za-z0-9_-]{16,}|\bgh[pousr]_[A-Za-z0-9]{20,}|\bgithub_pat_[A-Za-z0-9_]{20,}` +
	`|\bxox[abposr]-[A-Za-z0-9-]{10,}|\bAKIA[0-9A-Z]{16}\b|\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}` +
	`|\b[A-Za-z0-9_]*(?:token|secret|passw(?:or)?d|api_?key|access_?key)[A-Za-z0-9_]*=\S+`)

const redacted = "[redacted]"

func redact(s string) string {
	return tokenLike.ReplaceAllStringFunc(s, func(m string) string {
		if name, _, ok := strings.Cut(m, "="); ok && !strings.ContainsAny(name, " \t") {
			return name + "=" + redacted
		}
		return redacted
	})
}

// sanitize re-encodes raw token by token: values under env members are
// dropped, token-like strings redacted and content members cut. A changed
// recording gains "mdnav_record_truncated" or "mdnav_record_redacted":
// true. ok is false when raw is not one JSON value.
func sanitize(raw []byte) (out []byte, ok bool) {
	dec := jsontext.NewDecoder(bytes.NewReader(raw))
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf, jsontext.Multiline(true))
	names := map[int]string{} // the last member name at each depth
	envDepth := 0             // > 0 while inside an env member's value
	cut, red := false, false
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
		if envDepth > 0 && depth < envDepth {
			envDepth = 0 // left the env object
		}
		isName := tok.Kind() == '"' && kind == '{' && n%2 == 1
		switch {
		case isName:
			names[depth] = tok.String()
		case tok.Kind() == '{' && envFields[names[depth-1]] && envDepth == 0:
			envDepth = depth
		case tok.Kind() == '"' && envDepth > 0:
			tok, red = jsontext.String(redacted), true
		case tok.Kind() == '"':
			v := tok.String()
			if r := redact(v); r != v {
				v, red = r, true
			}
			if kind == '{' && contentFields[names[depth]] && len(v) > maxContent {
				v, cut = cutUTF8(v, maxContent), true
			}
			tok = jsontext.String(v)
		}
		if depth == 0 && tok.Kind() == '}' {
			for name, on := range map[string]bool{"mdnav_record_truncated": cut, "mdnav_record_redacted": red} {
				if on && (enc.WriteToken(jsontext.String(name)) != nil || enc.WriteToken(jsontext.True) != nil) {
					return nil, false
				}
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
