package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	src := `version: 1
defaults:
  conflict: backup
rules:
  - path: .env
    action: copy
  - path: certs/
    action: link
    link: absolute
  - path: node_modules/
    action: clone
    conflict: overwrite
`
	c, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	rules := c.Resolved()
	if len(rules) != 3 {
		t.Fatalf("rules = %d, want 3", len(rules))
	}
	want := []Rule{
		{Path: ".env", Action: ActionCopy, Conflict: ConflictBackup, Link: LinkRelative},
		{Path: "certs/", Action: ActionLink, Conflict: ConflictBackup, Link: LinkAbsolute},
		{Path: "node_modules/", Action: ActionClone, Conflict: ConflictOverwrite, Link: LinkRelative},
	}
	for i := range want {
		if rules[i] != want[i] {
			t.Errorf("rules[%d] = %+v, want %+v", i, rules[i], want[i])
		}
	}
	if !rules[1].DirOnly() || rules[1].Pattern() != "certs" {
		t.Errorf("DirOnly/Pattern mismatch: %v %s", rules[1].DirOnly(), rules[1].Pattern())
	}
	if rules[0].DirOnly() {
		t.Errorf(".env should not be DirOnly")
	}
}

func TestParseDebug(t *testing.T) {
	c, err := Parse([]byte("version: 1\ndebug: true\nrules: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Debug {
		t.Error("debug = false")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"version", "version: 2\nrules: []\n", "unsupported version"},
		{"no version", "rules: []\n", "unsupported version"},
		{"unknown action", "version: 1\nrules:\n  - path: a\n    action: move\n", "unknown action"},
		{"missing action", "version: 1\nrules:\n  - path: a\n", "action is required"},
		{"missing path", "version: 1\nrules:\n  - action: copy\n", "path is required"},
		{"absolute", "version: 1\nrules:\n  - path: /etc/hosts\n    action: copy\n", "must be relative"},
		{"parent", "version: 1\nrules:\n  - path: ../x\n    action: copy\n", "inside the worktree"},
		{"parent cleaned", "version: 1\nrules:\n  - path: a/../../x\n    action: copy\n", "inside the worktree"},
		{"root", "version: 1\nrules:\n  - path: ./\n    action: copy\n", "inside the worktree"},
		{"git dir", "version: 1\nrules:\n  - path: .git/hooks\n    action: copy\n", "into .git"},
		{"conflict", "version: 1\nrules:\n  - path: a\n    action: copy\n    conflict: merge\n", "unknown conflict"},
		{"link", "version: 1\nrules:\n  - path: a\n    action: link\n    link: hard\n", "unknown link"},
		{"defaults", "version: 1\ndefaults:\n  conflict: x\nrules: []\n", "defaults"},
		{"unknown field", "version: 1\nrule: []\n", "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.src))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %q, want contains %q", err, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(filepath.Join(dir, "none"))
	if err != nil || c != nil {
		t.Fatalf("missing file: c=%v err=%v", c, err)
	}

	p := filepath.Join(dir, FileName)
	if err := os.WriteFile(p, []byte("version: 1\nrules:\n  - path: a\n    action: bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), p) {
		t.Errorf("err = %v, want filename in message", err)
	}
}

func TestTemplateIsValid(t *testing.T) {
	c, err := Parse([]byte(Template))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Rules) != 0 {
		t.Errorf("template should have no active rules")
	}
}

func TestMerge(t *testing.T) {
	shared := &Config{
		Version:  1,
		Defaults: Defaults{Conflict: ConflictSkip, Link: LinkRelative},
		Rules: []Rule{
			{Path: ".env", Action: ActionCopy},
			{Path: "node_modules/", Action: ActionClone},
		},
	}
	private := &Config{
		Version:  1,
		Defaults: Defaults{Link: LinkAbsolute},
		Rules: []Rule{
			{Path: "node_modules/", Action: ActionLink},
			{Path: "certs/", Action: ActionLink},
		},
	}
	m := Merge(shared, private)
	if m.Defaults.Conflict != ConflictSkip || m.Defaults.Link != LinkAbsolute {
		t.Errorf("defaults = %+v", m.Defaults)
	}
	want := []Rule{
		{Path: ".env", Action: ActionCopy},
		{Path: "node_modules/", Action: ActionLink},
		{Path: "certs/", Action: ActionLink},
	}
	if len(m.Rules) != len(want) {
		t.Fatalf("rules = %+v", m.Rules)
	}
	for i := range want {
		if m.Rules[i] != want[i] {
			t.Errorf("rules[%d] = %+v, want %+v", i, m.Rules[i], want[i])
		}
	}

	if m.Debug {
		t.Error("debug should be off")
	}
	if !Merge(shared, &Config{Version: 1, Debug: true}).Debug {
		t.Error("private debug should enable debug")
	}

	if got := Merge(nil, nil); got == nil || len(got.Rules) != 0 {
		t.Errorf("Merge(nil, nil) = %+v", got)
	}
}
