package internal

// Verifies how a scalar-or-map "upstream_owned" entry behaves across the two
// marshaling paths gitspork uses:
//   1. goccy/go-yaml (yaml.Marshal / MarshalWithOptions) — the WriteGitSporkConfig path
//   2. rockholla/go-lib marshal.YAMLWithComments — the schema (GetGitSporkConfigSchema) path
//
// goccy honors a custom BytesMarshaler/BytesUnmarshaler, so it emits a bare scalar
// for plain entries and a {from,to} map for renames, and round-trips both. The
// reflection-based YAMLWithComments ignores custom marshalers and renders struct
// fields directly, so plain entries come out as `- pattern: "x"`; the schema output
// is post-processed (collapsePlainUpstreamOwned) to restore the bare-scalar form.
//
// The local entry/config types here are temporary fixtures mirroring the planned
// design; they will be replaced by the real UpstreamOwnedEntry type during
// implementation, and these tests will then exercise it directly.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/rockholla/go-lib/marshal"
)

type marshalTestEntry struct {
	Pattern string `yaml:"pattern,omitempty" comment:"a plain glob pattern"`
	From    string `yaml:"from,omitempty" comment:"rename source"`
	To      string `yaml:"to,omitempty" comment:"rename destination"`
}

func (e *marshalTestEntry) UnmarshalYAML(b []byte) error {
	var s string
	if err := yaml.Unmarshal(b, &s); err == nil {
		e.Pattern = s
		return nil
	}
	var m struct {
		From string `yaml:"from"`
		To   string `yaml:"to"`
	}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return err
	}
	e.From, e.To = m.From, m.To
	return nil
}

func (e marshalTestEntry) MarshalYAML() ([]byte, error) {
	if e.From != "" {
		return yaml.Marshal(yaml.MapSlice{
			{Key: "from", Value: e.From},
			{Key: "to", Value: e.To},
		})
	}
	return yaml.Marshal(e.Pattern)
}

type marshalTestConfig struct {
	UpstreamOwned   []marshalTestEntry `yaml:"upstream_owned" comment:"file patterns or {from,to} renames"`
	DownstreamOwned []string           `yaml:"downstream_owned" comment:"downstream-owned patterns"`
}

// collapsePlainUpstreamOwned rewrites reflection-rendered `- pattern: "X"` lines
// within the upstream_owned: block back to a bare scalar `- "X"`, leaving {from,to}
// rename entries and following sections untouched.
var patternLineRE = regexp.MustCompile(`^(\s*)- pattern: (".*?"|\S+)(\s*#.*)?$`)

func collapsePlainUpstreamOwned(schema string) string {
	lines := strings.Split(schema, "\n")
	inBlock := false
	for i, line := range lines {
		if strings.HasPrefix(line, "upstream_owned:") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		// A list item or its continuation stays in the block; a non-indented,
		// non-list, non-blank line is a new top-level key and ends the block.
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, " ") && strings.TrimSpace(line) != "" {
			inBlock = false
			continue
		}
		if m := patternLineRE.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + "- " + m[2]
		}
	}
	return strings.Join(lines, "\n")
}

// goccy unmarshal of a list mixing scalar and {from,to} map forms.
func TestUpstreamOwnedEntry_UnmarshalUnion(t *testing.T) {
	src := `upstream_owned:
  - src/**
  - from: .markdownlint-downstream.jsonc
    to: .markdownlint.jsonc
  - configs/**
`
	var cfg marshalTestConfig
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(cfg.UpstreamOwned) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(cfg.UpstreamOwned), cfg.UpstreamOwned)
	}
	if cfg.UpstreamOwned[0].Pattern != "src/**" {
		t.Errorf("entry0: want Pattern=src/**, got %+v", cfg.UpstreamOwned[0])
	}
	if cfg.UpstreamOwned[1].From != ".markdownlint-downstream.jsonc" || cfg.UpstreamOwned[1].To != ".markdownlint.jsonc" {
		t.Errorf("entry1: want from/to rename, got %+v", cfg.UpstreamOwned[1])
	}
	if cfg.UpstreamOwned[2].Pattern != "configs/**" {
		t.Errorf("entry2: want Pattern=configs/**, got %+v", cfg.UpstreamOwned[2])
	}
}

// goccy marshal round-trips plain entries to scalars and renames to {from,to} maps.
func TestUpstreamOwnedEntry_MarshalRoundTrip(t *testing.T) {
	cfg := marshalTestConfig{UpstreamOwned: []marshalTestEntry{
		{Pattern: "src/**"},
		{From: "a.txt", To: "b.txt"},
	}}
	out, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	t.Logf("goccy marshal output:\n%s", out)
	if !strings.Contains(string(out), "- src/**") {
		t.Errorf("plain entry did not marshal to a bare scalar:\n%s", out)
	}
	if !strings.Contains(string(out), "from: a.txt") || !strings.Contains(string(out), "to: b.txt") {
		t.Errorf("rename entry did not marshal to a {from,to} map:\n%s", out)
	}

	var back marshalTestConfig
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatalf("re-unmarshal failed: %v", err)
	}
	if back.UpstreamOwned[0].Pattern != "src/**" {
		t.Errorf("round-trip entry0 lost scalar: %+v", back.UpstreamOwned[0])
	}
	if back.UpstreamOwned[1].From != "a.txt" || back.UpstreamOwned[1].To != "b.txt" {
		t.Errorf("round-trip entry1 lost rename: %+v", back.UpstreamOwned[1])
	}
}

// The comment-preserving write path (MarshalWithOptions + CommentMap) keeps user
// comments and still renders scalar/map forms correctly.
func TestUpstreamOwnedEntry_MarshalPreservesComments(t *testing.T) {
	src := `upstream_owned:
  # a user comment on the first entry
  - src/**
  - from: a.txt
    to: b.txt
`
	var cfg marshalTestConfig
	cm := yaml.CommentMap{}
	if err := yaml.UnmarshalWithOptions([]byte(src), &cfg, yaml.CommentToMap(cm)); err != nil {
		t.Fatalf("unmarshal with comments failed: %v", err)
	}
	out, err := yaml.MarshalWithOptions(&cfg, yaml.WithComment(cm))
	if err != nil {
		t.Fatalf("marshal with comments failed: %v", err)
	}
	t.Logf("comment-preserving marshal output:\n%s", out)
	if !strings.Contains(string(out), "a user comment on the first entry") {
		t.Errorf("user comment was lost:\n%s", out)
	}
	if !strings.Contains(string(out), "- src/**") {
		t.Errorf("plain entry not rendered as scalar under comment path:\n%s", out)
	}
}

// Documents the reflection-based schema renderer's behavior: it ignores the custom
// MarshalYAML and emits plain entries in `- pattern:` map form.
func TestUpstreamOwnedSchema_ReflectionRendersMapForm(t *testing.T) {
	cfg := &marshalTestConfig{UpstreamOwned: []marshalTestEntry{
		{Pattern: "src/**"},
		{From: "a.txt", To: "b.txt"},
	}}
	out, err := marshal.YAMLWithComments(cfg, 0)
	if err != nil {
		t.Fatalf("YAMLWithComments failed: %v", err)
	}
	t.Logf("YAMLWithComments (schema) output:\n%s", out)
	if !strings.Contains(out, "- pattern:") {
		t.Errorf("expected reflection renderer to emit `- pattern:` map form, got:\n%s", out)
	}
}

// The chosen schema fix: post-process to collapse plain entries to scalars while
// leaving rename entries and the following section intact.
func TestUpstreamOwnedSchema_CollapsePlainEntries(t *testing.T) {
	cfg := &marshalTestConfig{
		UpstreamOwned: []marshalTestEntry{
			{Pattern: "upstream-owned.txt"},
			{From: ".markdownlint-downstream.jsonc", To: ".markdownlint.jsonc"},
		},
		DownstreamOwned: []string{"downstream-owned.md"},
	}
	raw, err := marshal.YAMLWithComments(cfg, 0)
	if err != nil {
		t.Fatalf("YAMLWithComments failed: %v", err)
	}
	out := collapsePlainUpstreamOwned(raw)
	t.Logf("post-processed schema:\n%s", out)

	if !strings.Contains(out, `- "upstream-owned.txt"`) {
		t.Errorf("plain entry was not collapsed to a scalar:\n%s", out)
	}
	if strings.Contains(out, "- pattern:") {
		t.Errorf("a `- pattern:` line survived collapsing:\n%s", out)
	}
	if !strings.Contains(out, `from: ".markdownlint-downstream.jsonc"`) || !strings.Contains(out, `to: ".markdownlint.jsonc"`) {
		t.Errorf("rename entry was damaged by collapsing:\n%s", out)
	}
	if !strings.Contains(out, `- "downstream-owned.md"`) {
		t.Errorf("downstream_owned section was altered:\n%s", out)
	}
}
