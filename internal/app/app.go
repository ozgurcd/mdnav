// Package app wires configuration, file loading, queries and rendering
// together for the mdnav command.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/ozgurcd/mdnav/internal/config"
	"github.com/ozgurcd/mdnav/internal/idpat"
	"github.com/ozgurcd/mdnav/internal/nav"
	"github.com/ozgurcd/mdnav/internal/render"
	"github.com/ozgurcd/mdnav/internal/source"
)

// Commands.
const (
	CmdGet     = "get"
	CmdOutline = "outline"
	CmdRefs    = "refs"
	CmdStatus  = "status"
	CmdIDs     = "ids"
)

// Request is one parsed command line.
type Request struct {
	Command    string
	Arg        string   // query, id or file, depending on Command
	Paths      []string // files and directories to search
	Options    nav.Options
	JSON       bool
	FilesOnly  bool   // get: print only the paths of matching files
	ConfigPath string // explicit .mdnav.json; "" means discover from the working directory
}

// ErrNoMatch is returned when a query matched nothing, so the caller can set
// a non-zero exit status.
var ErrNoMatch = errors.New("no match")

// Run executes req and writes the result to w.
func Run(ctx context.Context, req Request, w io.Writer) error {
	cfg, err := loadConfig(req.ConfigPath)
	if err != nil {
		return err
	}
	ids, err := idpat.New(cfg.Patterns)
	if err != nil {
		return err
	}
	paths := req.Paths
	if req.Command == CmdOutline {
		info, err := os.Stat(req.Arg)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("outline: %s is a directory; give one file", req.Arg)
		}
		paths = []string{req.Arg}
	}
	docs, err := source.Load(ctx, paths)
	if err != nil {
		return err
	}
	n := nav.New(docs, ids)

	switch req.Command {
	case CmdGet:
		r, err := n.Get(ctx, req.Arg, req.Options)
		if err != nil {
			return err
		}
		switch {
		case req.FilesOnly:
			err = writeFiles(w, r, req.JSON)
		case req.JSON:
			err = render.JSON(w, r)
		default:
			err = render.Get(w, r)
		}
		return noMatch(err, len(r.Blocks) == 0)
	case CmdOutline:
		r, err := n.Outline(ctx, docs[0].Path, req.Options)
		if err != nil {
			return err
		}
		return output(w, req.JSON, r, render.Outline)
	case CmdRefs:
		r, err := n.Refs(ctx, req.Arg, req.Options)
		if err != nil {
			return err
		}
		return noMatch(output(w, req.JSON, r, render.Refs), r.Total == 0)
	case CmdStatus:
		r, err := n.Status(ctx, req.Arg, req.Options)
		if err != nil {
			return err
		}
		return noMatch(output(w, req.JSON, r, render.Status), r.Mentions == 0)
	case CmdIDs:
		r, err := n.IDs(ctx, req.Options)
		if err != nil {
			return err
		}
		return output(w, req.JSON, r, render.IDs)
	}
	return fmt.Errorf("unknown command %q", req.Command)
}

func loadConfig(path string) (config.Config, error) {
	if path != "" {
		return config.Load(path)
	}
	return config.Discover(".")
}

func output[T any](w io.Writer, asJSON bool, r T, text func(io.Writer, T) error) error {
	if asJSON {
		return render.JSON(w, r)
	}
	return text(w, r)
}

func noMatch(err error, empty bool) error {
	if err == nil && empty {
		return ErrNoMatch
	}
	return err
}

func writeFiles(w io.Writer, r nav.GetResult, asJSON bool) error {
	files := []string{}
	for _, b := range r.Blocks {
		if !slices.Contains(files, b.Path) {
			files = append(files, b.Path)
		}
	}
	if asJSON {
		return render.JSON(w, files)
	}
	for _, f := range files {
		if _, err := fmt.Fprintln(w, f); err != nil {
			return err
		}
	}
	return nil
}
