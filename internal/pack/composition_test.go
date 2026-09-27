package pack

import (
	"os"
	"path/filepath"
	"testing"
)

func writePackYaml(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// aae-orc-soh8q: an installed pack reports its pin composition, or says
// plainly that it has none recorded. It never implies "current".
func TestLoadComposition_Summaries(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{
			name: "pinned as-of",
			body: "name: bmad\nversion: 6.12.0\nschema_version: 0.2.0\ncomposition:\n  pin_policy: as-of-release-date\n  as_of_date: \"2026-09-04T02:31:21.267Z\"\n  external_modules:\n    - name: cis\n      version: v0.3.2\n    - name: tea\n      version: v1.24.0\n",
			want: "external modules pinned as of 2026-09-04 (cis v0.3.2, tea v1.24.0)",
		},
		{
			name: "floating",
			body: "name: bmad\nschema_version: 0.2.0\ncomposition:\n  pin_policy: unpinned-floating\n  as_of_date: null\n  external_modules:\n    - name: tea\n      version: v1.26.0\n",
			want: "external modules not pinned (unpinned-floating)",
		},
		{
			name: "no externals",
			body: "name: x\nschema_version: 0.2.0\ncomposition:\n  pin_policy: as-of-release-date\n  as_of_date: \"2026-09-04T00:00:00Z\"\n  external_modules: []\n",
			want: "no external modules",
		},
		{
			name: "schema 0.1.0, no block",
			body: "name: bmad\nversion: 6.10.0\nschema_version: 0.1.0\n",
			want: "pin data not recorded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := LoadComposition(writePackYaml(t, tc.body))
			if err != nil {
				t.Fatalf("LoadComposition error: %v", err)
			}
			if got := c.Summary(); got != tc.want {
				t.Errorf("Summary() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadComposition_NoPackYaml(t *testing.T) {
	c, err := LoadComposition(t.TempDir())
	if err != nil {
		t.Fatalf("LoadComposition error: %v", err)
	}
	if got := c.Summary(); got != "pin data not recorded" {
		t.Errorf("Summary() = %q, want %q", got, "pin data not recorded")
	}
}

func TestLoadComposition_MalformedIsAnError(t *testing.T) {
	if _, err := LoadComposition(writePackYaml(t, "composition: [unclosed\n")); err == nil {
		t.Fatal("LoadComposition on malformed pack.yaml returned no error")
	}
}
