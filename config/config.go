// Package config loads and validates .gituntracked files.
package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/goccy/go-yaml"
)

// FileName is the name of the shared config file placed at the main worktree root.
const FileName = ".gituntracked"

// PrivateFileName is the name of the personal config file placed under $GIT_COMMON_DIR/info.
const PrivateFileName = "gituntracked"

// CurrentVersion is the config schema version supported by this build.
const CurrentVersion = 1

// Action is how a path is propagated to a worktree.
type Action string

// Actions
const (
	ActionCopy  Action = "copy"
	ActionLink  Action = "link"
	ActionClone Action = "clone"
)

// Conflict is how an existing, differing destination is handled.
type Conflict string

// Conflict policies
const (
	ConflictSkip      Conflict = "skip"
	ConflictOverwrite Conflict = "overwrite"
	ConflictBackup    Conflict = "backup"
)

// LinkMode is whether symlinks are created with relative or absolute targets.
type LinkMode string

// Link modes
const (
	LinkRelative LinkMode = "relative"
	LinkAbsolute LinkMode = "absolute"
)

// Defaults holds values applied to rules that do not set them.
type Defaults struct {
	Conflict Conflict `yaml:"conflict,omitempty"`
	Link     LinkMode `yaml:"link,omitempty"`
}

// Rule is a single path to propagate.
type Rule struct {
	Path     string   `yaml:"path"`
	Action   Action   `yaml:"action"`
	Conflict Conflict `yaml:"conflict,omitempty"`
	Link     LinkMode `yaml:"link,omitempty"`
}

// DirOnly reports whether the rule only matches directories (trailing slash).
func (r Rule) DirOnly() bool {
	return strings.HasSuffix(r.Path, "/")
}

// Pattern returns the path without the trailing slash.
func (r Rule) Pattern() string {
	return strings.TrimSuffix(r.Path, "/")
}

// Config is the content of a .gituntracked file.
type Config struct {
	Version  int      `yaml:"version"`
	Debug    bool     `yaml:"debug,omitempty"`
	Defaults Defaults `yaml:"defaults,omitempty"`
	Rules    []Rule   `yaml:"rules"`
}

// Load reads a config file. It returns (nil, nil) when the file does not exist.
func Load(filename string) (*Config, error) {
	b, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	return c, nil
}

// Parse decodes and validates config bytes.
func Parse(b []byte) (*Config, error) {
	var c Config
	if err := yaml.UnmarshalWithOptions(b, &c, yaml.Strict()); err != nil {
		return nil, errors.New(yaml.FormatError(err, false, true))
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks the config for unsupported values.
func (c *Config) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("unsupported version: %d (supported: %d)", c.Version, CurrentVersion)
	}
	if err := validateConflict(c.Defaults.Conflict); err != nil {
		return fmt.Errorf("defaults: %w", err)
	}
	if err := validateLink(c.Defaults.Link); err != nil {
		return fmt.Errorf("defaults: %w", err)
	}
	for i, r := range c.Rules {
		if err := r.validate(); err != nil {
			return fmt.Errorf("rules[%d]: %w", i, err)
		}
	}
	return nil
}

func (r Rule) validate() error {
	if r.Path == "" {
		return errors.New("path is required")
	}
	if strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("path must be relative: %s", r.Path)
	}
	p := path.Clean(r.Pattern())
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("path must be inside the worktree: %s", r.Path)
	}
	if p == ".git" || strings.HasPrefix(p, ".git/") {
		return fmt.Errorf("path must not point into .git: %s", r.Path)
	}
	switch r.Action {
	case ActionCopy, ActionLink, ActionClone:
	case "":
		return errors.New("action is required")
	default:
		return fmt.Errorf("unknown action: %s (copy, link, clone)", r.Action)
	}
	if err := validateConflict(r.Conflict); err != nil {
		return err
	}
	return validateLink(r.Link)
}

func validateConflict(v Conflict) error {
	switch v {
	case "", ConflictSkip, ConflictOverwrite, ConflictBackup:
		return nil
	}
	return fmt.Errorf("unknown conflict: %s (skip, overwrite, backup)", v)
}

func validateLink(v LinkMode) error {
	switch v {
	case "", LinkRelative, LinkAbsolute:
		return nil
	}
	return fmt.Errorf("unknown link: %s (relative, absolute)", v)
}

// Merge overlays private onto shared. Rules with the same path are replaced
// in place, new rules are appended. Debug is enabled when either enables it.
// Either argument may be nil.
func Merge(shared, private *Config) *Config {
	out := &Config{Version: CurrentVersion}
	for _, c := range []*Config{shared, private} {
		if c == nil {
			continue
		}
		out.Debug = out.Debug || c.Debug
		if c.Defaults.Conflict != "" {
			out.Defaults.Conflict = c.Defaults.Conflict
		}
		if c.Defaults.Link != "" {
			out.Defaults.Link = c.Defaults.Link
		}
		for _, r := range c.Rules {
			replaced := false
			for i := range out.Rules {
				if out.Rules[i].Path == r.Path {
					out.Rules[i] = r
					replaced = true
					break
				}
			}
			if !replaced {
				out.Rules = append(out.Rules, r)
			}
		}
	}
	return out
}

// Resolved returns the rules with defaults filled in.
func (c *Config) Resolved() []Rule {
	conflict := c.Defaults.Conflict
	if conflict == "" {
		conflict = ConflictSkip
	}
	link := c.Defaults.Link
	if link == "" {
		link = LinkRelative
	}
	rules := make([]Rule, len(c.Rules))
	for i, r := range c.Rules {
		if r.Conflict == "" {
			r.Conflict = conflict
		}
		if r.Link == "" {
			r.Link = link
		}
		rules[i] = r
	}
	return rules
}

// Template is written by `git untracked init`.
const Template = `# git-untracked: files propagated from the main worktree to other worktrees.
# See https://github.com/linyows/git-untracked
version: 1
# debug: true       # log to $GIT_COMMON_DIR/git-untracked.log
defaults:
  conflict: skip    # skip | overwrite | backup
  link: relative    # relative | absolute
rules:
  # - path: .env
  #   action: copy  # copy | link | clone
  # - path: certs/
  #   action: link
  # - path: node_modules/
  #   action: clone
`
