package app

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"

	"github.com/ozgurcd/mdnav/internal/edit"
	"github.com/ozgurcd/mdnav/internal/idpat"
	"github.com/ozgurcd/mdnav/internal/markdown"
	"github.com/ozgurcd/mdnav/internal/nav"
	"github.com/ozgurcd/mdnav/internal/render"
	"github.com/ozgurcd/mdnav/internal/source"
)

// maxInput bounds --from input and edits files read by apply.
const maxInput = 16 << 20

func isEdit(cmd string) bool {
	return slices.Contains([]string{CmdSub, CmdSet, CmdReplace, CmdInsert, CmdDelete, CmdAddRow, CmdApply}, cmd)
}

// EditFile is the JSON document `mdnav apply` reads: edits to one file,
// applied in order, all or nothing. Unknown fields are rejected.
type EditFile struct {
	File  string    `json:"file"`
	Edits []edit.Op `json:"edits"`
}

var pathLineRe = regexp.MustCompile(`^(.+):[0-9]+$`)

func runEdit(ctx context.Context, req Request, ids *idpat.Matcher, w io.Writer) error {
	if req.Command == CmdApply {
		return runApply(ctx, req, ids, w)
	}
	op := req.Edit
	if req.From != "" {
		lines, err := readLines(req.From, req.Stdin)
		if err != nil {
			return err
		}
		op.Lines = lines
	}

	// Find the one file the target is in, then let edit re-find the block
	// under that file's lock.
	paths := req.Paths
	if m := pathLineRe.FindStringSubmatch(op.Target); m != nil && len(paths) == 0 {
		if info, err := os.Stat(m[1]); err == nil && info.Mode().IsRegular() {
			paths = []string{m[1]}
		}
	}
	docs, err := source.Load(ctx, paths)
	if err != nil {
		return err
	}
	t, err := nav.New(docs, ids).Resolve(ctx, op.Target, nav.Options{})
	if err != nil {
		return err
	}
	res, err := edit.Apply(ctx, ids, t.Path, []edit.Op{op}, req.DryRun)
	if err != nil {
		return err
	}
	return output(w, req.JSON, res, render.Edit)
}

func runApply(ctx context.Context, req Request, ids *idpat.Matcher, w io.Writer) error {
	data, err := readFile(req.Arg, req.Stdin)
	if err != nil {
		return err
	}
	var ef EditFile
	if err := json.Unmarshal(data, &ef, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("%s: %w", req.Arg, err)
	}
	if ef.File == "" {
		return fmt.Errorf("%s: \"file\" is required", req.Arg)
	}
	res, err := edit.Apply(ctx, ids, ef.File, ef.Edits, req.DryRun)
	if err != nil {
		return err
	}
	return output(w, req.JSON, res, render.Edit)
}

func readLines(path string, stdin io.Reader) ([]string, error) {
	data, err := readFile(path, stdin)
	if err != nil {
		return nil, err
	}
	return markdown.SplitLines(data), nil
}

// readFile reads path, or stdin for "-", up to maxInput bytes.
func readFile(path string, stdin io.Reader) ([]byte, error) {
	var r io.Reader
	if path == "-" {
		if stdin == nil {
			return nil, fmt.Errorf("no standard input")
		}
		r = stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	var buf bytes.Buffer
	if _, err := io.CopyN(&buf, r, maxInput+1); err != nil && err != io.EOF {
		return nil, err
	}
	if buf.Len() > maxInput {
		return nil, fmt.Errorf("%s: input over %d bytes", path, maxInput)
	}
	return buf.Bytes(), nil
}
