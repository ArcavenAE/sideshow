package weave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func loadFrom(t *testing.T, body string) *Declaration {
	t.Helper()
	p := filepath.Join(t.TempDir(), "weave.yaml")
	write(t, p, body)
	d, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return d
}

// --- schema and vars ---

func TestLoadRequiresSchemaVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "weave.yaml")
	write(t, p, "pack: bmad\n")
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("want schema_version error, got %v", err)
	}
}

func TestLoadRejectsUnsupportedMinor(t *testing.T) {
	p := filepath.Join(t.TempDir(), "weave.yaml")
	write(t, p, "schema_version: 0.9.0\npack: bmad\n")
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("want unsupported-version error, got %v", err)
	}
}

// An unknown key inside a recognized operation must be an error: silently
// ignoring a misspelled on_missing_anchor would mean running the destructive
// default.
func TestLoadRejectsUnknownKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "weave.yaml")
	write(t, p, `schema_version: 0.1.0
pack: bmad
memory_injections:
  - targets: [a.yaml]
    anchor: "# x"
    on_missing_anchr: skip
    memories: [m]
`)
	if _, err := Load(p); err == nil {
		t.Fatal("want error for misspelled on_missing_anchor, got nil")
	}
}

// An unresolved var is an error, not an empty string. A silent empty
// substitution writes to a path one segment short of the intended one.
func TestUnresolvedVarIsAnError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "weave.yaml")
	write(t, p, `schema_version: 0.1.0
pack: bmad
slash_commands:
  - id: cmd-{{nope}}
    target: .claude/commands/cmd-{{nope}}.md
    body: "x"
`)
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "unresolved var") {
		t.Fatalf("want unresolved-var error, got %v", err)
	}
}

func TestVarsSubstituted(t *testing.T) {
	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
vars:
  project: midway
slash_commands:
  - id: bmad-agent-custom-{{project}}-sam
    target: .claude/commands/bmad-agent-custom-{{project}}-sam.md
    body: "x"
`)
	if got := d.SlashCommands[0].ID; got != "bmad-agent-custom-midway-sam" {
		t.Fatalf("id = %q", got)
	}
	if got := d.SlashCommands[0].Target; !strings.Contains(got, "midway") {
		t.Fatalf("target = %q", got)
	}
}

// --- csv_injections ---

func TestCSVAppendsRowsVerbatim(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "m.csv")
	write(t, target, "header\n")

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
csv_injections:
  - name: m
    target: m.csv
    guard: '"pe"'
    rows:
      - |-
        "pe","Sam",  "untrimmed"
`)
	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Applied {
		t.Fatalf("outcome = %v (%s)", res.Actions[0].Outcome, res.Actions[0].Detail)
	}
	want := "header\n\"pe\",\"Sam\",  \"untrimmed\"\n"
	if got := read(t, target); got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestCSVGuardMakesItIdempotent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "m.csv")
	write(t, target, "header\n")

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
csv_injections:
  - name: m
    target: m.csv
    guard: '"pe"'
    rows: ['"pe","Sam"']
`)
	if _, err := Apply(d, root, Options{}); err != nil {
		t.Fatal(err)
	}
	first := read(t, target)

	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Skipped {
		t.Fatalf("second run outcome = %v, want skipped", res.Actions[0].Outcome)
	}
	if got := read(t, target); got != first {
		t.Fatalf("second run changed content:\n%q\n%q", first, got)
	}
}

// Without normalization the first appended row fuses onto the last existing
// row. None of the five shell scripts handled this.
func TestCSVNormalizesMissingTrailingNewline(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "m.csv")
	write(t, target, "header\nlast,row,no,newline")

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
csv_injections:
  - name: m
    target: m.csv
    guard: 'ZZZ'
    rows: ['new,row']
`)
	if _, err := Apply(d, root, Options{}); err != nil {
		t.Fatal(err)
	}
	got := read(t, target)
	if !strings.HasSuffix(got, "no,newline\nnew,row\n") {
		t.Fatalf("rows fused or malformed: %q", got)
	}
}

func TestCSVMissingTargetFails(t *testing.T) {
	root := t.TempDir()
	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
csv_injections:
  - name: m
    target: absent.csv
    guard: x
    rows: ['a']
`)
	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Failed {
		t.Fatalf("outcome = %v, want failed", res.Actions[0].Outcome)
	}
}

// --- memory_injections ---

const customizeFixture = `# Agent customization
agent_name: bmm-architect

memories:
  - "stale one"
  - "stale two"

# Add custom menu
menu: []
`

func TestMemoryReplacesBlockBeforeAnchor(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.customize.yaml")
	write(t, target, customizeFixture)

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
memory_injections:
  - targets: [a.customize.yaml]
    anchor: '# Add custom menu'
    memories:
      - fresh one
      - fresh two
`)
	if _, err := Apply(d, root, Options{}); err != nil {
		t.Fatal(err)
	}
	got := read(t, target)

	if strings.Contains(got, "stale") {
		t.Fatalf("stale memories survived:\n%s", got)
	}
	if !strings.Contains(got, `  - "fresh one"`) {
		t.Fatalf("fresh memories not double-quoted:\n%s", got)
	}
	mi := strings.Index(got, "memories:")
	ai := strings.Index(got, "# Add custom menu")
	if mi < 0 || ai < 0 || mi > ai {
		t.Fatalf("block not inserted before anchor:\n%s", got)
	}
	if !strings.Contains(got, "agent_name: bmm-architect") || !strings.Contains(got, "menu: []") {
		t.Fatalf("surrounding content lost:\n%s", got)
	}
}

func TestMemoryIsIdempotent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.customize.yaml")
	write(t, target, customizeFixture)

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
memory_injections:
  - targets: [a.customize.yaml]
    anchor: '# Add custom menu'
    memories: [one]
`)
	if _, err := Apply(d, root, Options{}); err != nil {
		t.Fatal(err)
	}
	first := read(t, target)

	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, target); got != first {
		t.Fatalf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first, got)
	}
	if res.Actions[0].Outcome != Skipped {
		t.Fatalf("second run outcome = %v, want skipped", res.Actions[0].Outcome)
	}
}

// The shell awk deletes the memories block and then never reaches an insertion
// point, so the memories are lost and the run still reports success. The engine
// must leave the file byte-identical and report the failure.
func TestMemoryMissingAnchorLeavesFileUnchanged(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.customize.yaml")
	original := "agent_name: x\n\nmemories:\n  - \"keep me\"\n\nmenu: []\n"
	write(t, target, original)

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
memory_injections:
  - targets: [a.customize.yaml]
    anchor: '# Add custom menu'
    on_missing_anchor: fail
    memories: [new]
`)
	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Failed {
		t.Fatalf("outcome = %v, want failed", res.Actions[0].Outcome)
	}
	if got := read(t, target); got != original {
		t.Fatalf("file was mutated despite missing anchor:\n%q", got)
	}
}

func TestMemoryMissingAnchorSkipMode(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.customize.yaml")
	write(t, target, "agent_name: x\n")

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
memory_injections:
  - targets: [a.customize.yaml]
    anchor: '# nope'
    on_missing_anchor: skip
    memories: [new]
`)
	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Skipped {
		t.Fatalf("outcome = %v, want skipped", res.Actions[0].Outcome)
	}
}

// memories: [] must be treated as a block start, matching /^memories:/. The
// awk's separate /^memories: \[\]/ rule is unreachable for the same reason.
func TestMemoryHandlesInlineEmptyBlock(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.customize.yaml")
	write(t, target, "agent_name: x\nmemories: []\n# Add custom menu\nmenu: []\n")

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
memory_injections:
  - targets: [a.customize.yaml]
    anchor: '# Add custom menu'
    memories: [one]
`)
	if _, err := Apply(d, root, Options{}); err != nil {
		t.Fatal(err)
	}
	got := read(t, target)
	if strings.Contains(got, "memories: []") {
		t.Fatalf("empty inline block not replaced:\n%s", got)
	}
	if strings.Count(got, "memories:") != 1 {
		t.Fatalf("want exactly one memories key:\n%s", got)
	}
}

func TestMemoryEscapesQuotes(t *testing.T) {
	got := renderMemories([]string{`he said "hi"`, `back\slash`})
	if got[1] != `  - "he said \"hi\""` {
		t.Fatalf("quote escaping: %q", got[1])
	}
	if got[2] != `  - "back\\slash"` {
		t.Fatalf("backslash escaping: %q", got[2])
	}
}

// --- slash_commands ---

func TestShimWritesThenSkips(t *testing.T) {
	root := t.TempDir()
	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
slash_commands:
  - id: c
    target: .claude/commands/c.md
    body: |
      ---
      name: 'x'
      ---
`)
	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Applied {
		t.Fatalf("first outcome = %v (%s)", res.Actions[0].Outcome, res.Actions[0].Detail)
	}

	target := filepath.Join(root, ".claude/commands/c.md")
	write(t, target, "hand edited\n")
	res, err = Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Skipped {
		t.Fatalf("second outcome = %v, want skipped", res.Actions[0].Outcome)
	}
	if got := read(t, target); got != "hand edited\n" {
		t.Fatalf("hand edit was lost: %q", got)
	}
}

func TestShimOverwriteMode(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".claude/commands/c.md"), "old\n")
	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
slash_commands:
  - id: c
    target: .claude/commands/c.md
    on_exists: overwrite
    body: "new\n"
`)
	if _, err := Apply(d, root, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, ".claude/commands/c.md")); got != "new\n" {
		t.Fatalf("content = %q", got)
	}
}

// --- apply_upstream_patches ---

func TestPatchesNoneIsAccepted(t *testing.T) {
	d := loadFrom(t, "schema_version: 0.1.0\npack: bmad\napply_upstream_patches: none\n")
	res, err := Apply(d, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 0 {
		t.Fatalf("want no actions, got %+v", res.Actions)
	}
}

func TestPatchesAnythingElseFailsLoudly(t *testing.T) {
	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
apply_upstream_patches:
  - target: x.md
`)
	res, err := Apply(d, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 1 || res.Actions[0].Outcome != Failed {
		t.Fatalf("want one failed action, got %+v", res.Actions)
	}
}

// --- verification ---

func TestVerificationWarnsByDefaultAndErrorsUnderStrict(t *testing.T) {
	root := t.TempDir()
	body := `schema_version: 0.1.0
pack: bmad
verification:
  on_failure: warn
  required_files: [absent.md]
`
	d := loadFrom(t, body)

	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatalf("warn mode returned an error: %v", err)
	}
	if res.Actions[0].Outcome != Skipped {
		t.Fatalf("warn outcome = %v, want skipped", res.Actions[0].Outcome)
	}

	d2 := loadFrom(t, body)
	if _, err := Apply(d2, root, Options{Strict: true}); err == nil {
		t.Fatal("strict mode did not return an error")
	}
}

// --- orchestration ---

func TestFixedOrderAndFailuresDoNotAbort(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.customize.yaml"), customizeFixture)

	// The CSV target is deliberately absent. A missing bmad-help.csv must not
	// prevent the memories from being injected.
	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
csv_injections:
  - name: m
    target: absent.csv
    guard: x
    rows: ['a']
memory_injections:
  - targets: [a.customize.yaml]
    anchor: '# Add custom menu'
    memories: [one]
slash_commands:
  - id: c
    target: .claude/commands/c.md
    body: "x\n"
`)
	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var order []string
	for _, a := range res.Actions {
		order = append(order, a.Type)
	}
	want := []string{"csv", "memory", "shim"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if res.Actions[0].Outcome != Failed {
		t.Fatalf("csv should have failed, got %v", res.Actions[0].Outcome)
	}
	if res.Actions[1].Outcome != Applied {
		t.Fatalf("memory should still have applied, got %v (%s)",
			res.Actions[1].Outcome, res.Actions[1].Detail)
	}
	if !strings.Contains(read(t, filepath.Join(root, "a.customize.yaml")), `- "one"`) {
		t.Fatal("memories were not written after the earlier failure")
	}

	applied, skipped, failed := res.Counts()
	if applied != 2 || skipped != 0 || failed != 1 {
		t.Fatalf("counts = %d/%d/%d, want 2/0/1", applied, skipped, failed)
	}
}

func TestDryRunTouchesNothing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "m.csv")
	write(t, target, "header\n")

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
csv_injections:
  - name: m
    target: m.csv
    guard: x
    rows: ['a']
`)
	res, err := Apply(d, root, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Applied {
		t.Fatalf("dry run should report applied, got %v", res.Actions[0].Outcome)
	}
	if got := read(t, target); got != "header\n" {
		t.Fatalf("dry run wrote to disk: %q", got)
	}
}

func TestApplyForPackAbsentDeclarationIsNotAnError(t *testing.T) {
	_, found, err := ApplyForPack(t.TempDir(), "bmad", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("found = true for a repo with no weave.yaml")
	}
}

func TestApplyForPackRejectsPackMismatch(t *testing.T) {
	root := t.TempDir()
	write(t, DeclarationPath(root, "bmad"), "schema_version: 0.1.0\npack: vsdd\n")
	_, found, err := ApplyForPack(root, "bmad", Options{})
	if !found {
		t.Fatal("found = false")
	}
	if err == nil || !strings.Contains(err.Error(), "declares pack") {
		t.Fatalf("want pack-mismatch error, got %v", err)
	}
}
