package bindings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newFixtureStore builds a frozen-store stand-in containing exactly the
// relative paths given (a trailing slash makes a directory), plus an
// optional pack.yaml body. It returns the store root.
func newFixtureStore(t *testing.T, packYAML string, entries ...string) string {
	t.Helper()

	root := t.TempDir()
	for _, e := range entries {
		full := filepath.Join(root, filepath.FromSlash(e))
		if strings.HasSuffix(e, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", e, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir parent of %s: %v", e, err)
		}
		if err := os.WriteFile(full, []byte("fixture\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", e, err)
		}
	}
	if packYAML != "" {
		if err := os.WriteFile(filepath.Join(root, "pack.yaml"), []byte(packYAML), 0o644); err != nil {
			t.Fatalf("write pack.yaml: %v", err)
		}
	}
	return root
}

const bridgePackYAML = `name: bmad
version: 6.10.0
distribute:
  custom_bridge:
    upstream_path: _bmad/custom
    per_repo_dir: _bmad-custom
`

func TestRewrite_Classification(t *testing.T) {
	t.Parallel()

	// The store carries pack content only. config.yaml, module-help.csv
	// and memory/ are absent because they are project state — exactly the
	// shape of a real user-install (aae-orc-c8v8).
	store := newFixtureStore(t, bridgePackYAML,
		"bmm/agents/pm.md",
		"scripts/merge-config.py",
		"core/workflows/party-mode/",
		"custom/config.toml",
	)
	rules := newPackRefRules(store, "_bmad")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "pack content rewrites",
			in:   "Load from {project-root}/_bmad/bmm/agents/pm.md",
			want: "Load from " + store + "/bmm/agents/pm.md",
		},
		{
			name: "project state stays literal",
			in:   "write {project-root}/_bmad/config.yaml",
			want: "write {project-root}/_bmad/config.yaml",
		},
		{
			name: "declared per-repo surface stays literal even though it exists",
			in:   "Create {project-root}/_bmad/custom/ if needed.",
			want: "Create {project-root}/_bmad/custom/ if needed.",
		},
		{
			name: "per-repo surface stays literal for a concrete file",
			in:   "read {project-root}/_bmad/custom/config.toml",
			want: "read {project-root}/_bmad/custom/config.toml",
		},
		{
			name: "placeholder tail resolves against its literal prefix",
			in:   "see {project-root}/_bmad/core/workflows/{name}/workflow.md",
			want: "see " + store + "/core/workflows/{name}/workflow.md",
		},
		{
			name: "absent directory under a placeholder stays literal",
			in:   "sanctum at {project-root}/_bmad/memory/{skillName}/",
			want: "sanctum at {project-root}/_bmad/memory/{skillName}/",
		},
		{
			name: "sibling per-repo dirs still preserved",
			in:   "{project-root}/_bmad-custom/x {project-root}/_bmad-output/y",
			want: "{project-root}/_bmad-custom/x {project-root}/_bmad-output/y",
		},
		{
			name: "unrelated project paths untouched",
			in:   "Read {project-root}/docs/readme.md",
			want: "Read {project-root}/docs/readme.md",
		},
		{
			name: "mixed content classifies per reference",
			in: "Load {project-root}/_bmad/scripts/merge-config.py " +
				"then write {project-root}/_bmad/module-help.csv",
			want: "Load " + store + "/scripts/merge-config.py " +
				"then write {project-root}/_bmad/module-help.csv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := rules.rewrite(tt.in); got != tt.want {
				t.Errorf("rewrite:\n got  %q\n want %q", got, tt.want)
			}
		})
	}
}

// The fixture the ticket names: bmad-bmb-setup SKILL.md line 48, where
// three write targets were redirected into the frozen store.
func TestRewrite_BmbSetupLine48(t *testing.T) {
	t.Parallel()

	store := newFixtureStore(t, bridgePackYAML, "scripts/merge-config.py")
	rules := newPackRefRules(store, "_bmad")

	in := `python3 ./scripts/merge-config.py --config-path "{project-root}/_bmad/config.yaml" ` +
		`--user-config-path "{project-root}/_bmad/config.user.yaml" ` +
		`--answers {temp-file} --legacy-dir "{project-root}/_bmad"`

	got := rules.rewrite(in)
	if got != in {
		t.Errorf("write targets were redirected into the pack store:\n got  %q\n want %q", got, in)
	}
	if strings.Contains(got, store) {
		t.Errorf("rewrite leaked the store path into a write argument: %q", got)
	}
	if err := rules.verify(got); err != nil {
		t.Errorf("verify rejected correctly-classified content: %v", err)
	}
}

func TestVerify(t *testing.T) {
	t.Parallel()

	store := newFixtureStore(t, "", "bmm/agents/pm.md", "custom/")
	rules := newPackRefRules(store, "_bmad")

	tests := []struct {
		name    string
		in      string
		wantErr bool
		wantRef string
	}{
		{
			name: "references that exist pass",
			in:   "load " + store + "/bmm/agents/pm.md and " + store + "/custom/",
		},
		{
			name: "bare store root passes",
			in:   "the pack lives at " + store + "/",
		},
		{
			name:    "absent path is caught",
			in:      "write " + store + "/config.yaml",
			wantErr: true,
			wantRef: "config.yaml",
		},
		{
			name:    "absent path under a placeholder is caught",
			in:      "sanctum at " + store + "/memory/{skillName}/",
			wantErr: true,
			wantRef: "memory/",
		},
		{
			name: "paths outside this store are not our business",
			in:   "see /some/other/pack/config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := rules.verify(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("verify accepted a dangling reference: %q", tt.in)
				}
				if !errors.Is(err, ErrDanglingPackRefs) {
					t.Errorf("verify error is not ErrDanglingPackRefs: %v", err)
				}
				if !strings.Contains(err.Error(), tt.wantRef) {
					t.Errorf("verify error does not name %q: %v", tt.wantRef, err)
				}
				return
			}
			if err != nil {
				t.Errorf("verify rejected valid content %q: %v", tt.in, err)
			}
		})
	}
}

// verify runs independently of rewrite, so a reference that arrived
// absolute in the pack source is caught rather than trusted.
func TestVerify_CatchesPreRewrittenAbsoluteRef(t *testing.T) {
	t.Parallel()

	store := newFixtureStore(t, "", "bmm/agents/pm.md")
	rules := newPackRefRules(store, "_bmad")

	content := "--target " + store + "/module-help.csv"
	if rules.rewrite(content) != content {
		t.Fatal("precondition: rewrite should not touch an already-absolute reference")
	}
	if err := rules.verify(content); err == nil {
		t.Error("verify accepted an absolute dangling reference the rewrite never produced")
	}
}

func TestDeclaredPerRepoDirs(t *testing.T) {
	t.Parallel()

	t.Run("bridge declared", func(t *testing.T) {
		t.Parallel()
		store := newFixtureStore(t, bridgePackYAML)
		got := declaredPerRepoDirs(store, "_bmad")
		if _, ok := got["custom"]; !ok || len(got) != 1 {
			t.Errorf("declaredPerRepoDirs = %v, want {custom}", got)
		}
	})

	t.Run("no pack.yaml yields no preserved dirs", func(t *testing.T) {
		t.Parallel()
		store := newFixtureStore(t, "")
		if got := declaredPerRepoDirs(store, "_bmad"); len(got) != 0 {
			t.Errorf("declaredPerRepoDirs = %v, want empty", got)
		}
	})
}

func TestLiteralPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		{"bmm/agents/pm.md", "bmm/agents/pm.md"},
		{"memory/{skillName}/", "memory/"},
		{"bmm/agent-{code}.md", "bmm/"},
		{"{skill-name}.toml", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := literalPrefix(tt.in); got != tt.want {
			t.Errorf("literalPrefix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRefCandidate(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		{"bmm/pm.md and more", "bmm/pm.md"},
		{"bmm/pm.md`", "bmm/pm.md"},
		{`config.yaml" --next`, "config.yaml"},
		{"custom/ if needed.", "custom/"},
		{"planning/prd.md.", "planning/prd.md"},
		{"scripts/x.py)", "scripts/x.py"},
	}
	for _, tt := range tests {
		if got := refCandidate(tt.in); got != tt.want {
			t.Errorf("refCandidate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAppendFallbackFooter_AddsFooter(t *testing.T) {
	t.Parallel()

	input := "Some command content.\n"
	got := appendFallbackFooter(input, "/global/packs/bmad/6.2.2")

	if !strings.Contains(got, "<!-- sideshow:fallback-resolution:begin -->") {
		t.Errorf("appendFallbackFooter did not add begin marker: %q", got)
	}
	if !strings.Contains(got, "<!-- sideshow:fallback-resolution:end -->") {
		t.Errorf("appendFallbackFooter did not add end marker: %q", got)
	}
	if !strings.Contains(got, "/global/packs/bmad/6.2.2") {
		t.Errorf("appendFallbackFooter did not interpolate pack path: %q", got)
	}
	if !strings.HasPrefix(got, input) {
		t.Errorf("appendFallbackFooter did not preserve original content prefix")
	}
}

func TestAppendFallbackFooter_Idempotent(t *testing.T) {
	t.Parallel()

	input := "Some command content.\n"
	once := appendFallbackFooter(input, "/global/packs/bmad/6.2.2")
	twice := appendFallbackFooter(once, "/global/packs/bmad/6.2.2")

	if once != twice {
		t.Errorf("appendFallbackFooter is not idempotent:\n  once: %q\n  twice: %q", once, twice)
	}

	beginCount := strings.Count(twice, "<!-- sideshow:fallback-resolution:begin -->")
	if beginCount != 1 {
		t.Errorf("second call added another begin marker; count=%d", beginCount)
	}
}
