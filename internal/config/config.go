// Package config loads the optional .mdnav.json file.
//
// Example:
//
//	{
//	  "patterns":       [{"name": "D", "regex": "D\\d+"}],
//	  "extra_patterns": [{"name": "ADR", "regex": "ADR-\\d+"}]
//	}
//
// "patterns", when present, replaces the built-in identifier patterns;
// "extra_patterns" is appended to whichever set is in force. Unknown fields
// are rejected.
package config

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ozgurcd/mdnav/internal/idpat"
)

// FileName is the name of the configuration file.
const FileName = ".mdnav.json"

// File is the on-disk configuration.
type File struct {
	Patterns      []idpat.Pattern `json:"patterns,omitzero"`
	ExtraPatterns []idpat.Pattern `json:"extra_patterns,omitzero"`
}

// Config is the resolved configuration.
type Config struct {
	Source   string // path of the file it came from, or "" for built-in defaults
	Patterns []idpat.Pattern
}

// Default returns the built-in configuration.
func Default() Config { return Config{Patterns: idpat.Builtin()} }

// Parse decodes a configuration file's contents.
func Parse(data []byte) (Config, error) {
	var f File
	if err := json.Unmarshal(data, &f, json.RejectUnknownMembers(true)); err != nil {
		return Config{}, err
	}
	c := Default()
	if f.Patterns != nil {
		if len(f.Patterns) == 0 {
			return Config{}, errors.New(`"patterns" is empty; omit it to keep the built-in patterns`)
		}
		c.Patterns = f.Patterns
	}
	c.Patterns = append(c.Patterns, f.ExtraPatterns...)
	if _, err := idpat.New(c.Patterns); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Load reads the configuration at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	c, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	c.Source = path
	return c, nil
}

// Discover looks for FileName in dir and its parents. It returns the built-in
// defaults when none is found.
func Discover(dir string) (Config, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Config{}, err
	}
	for {
		p := filepath.Join(abs, FileName)
		if _, err := os.Stat(p); err == nil {
			return Load(p)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Config{}, err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return Default(), nil
		}
		abs = parent
	}
}
