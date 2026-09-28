package enable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeWithTwoVersions lays out a store the way install leaves it:
// both versions under packs/<name>/, current pointing at the active one,
// and a single registry row for the pack (aae-orc-15rhc).
func storeWithTwoVersions(t *testing.T) string {
	t.Helper()
	store := t.TempDir()
	t.Setenv("SIDESHOW_HOME", store)
	t.Setenv("HOME", t.TempDir())
	base := filepath.Join(store, "packs", "vsdd-factory")
	for _, v := range []string{"1.0.0-rc.24", "1.0.0-rc.25"} {
		dir := filepath.Join(base, v)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		py := fmt.Sprintf("name: vsdd-factory\nversion: %s\nactivation:\n  mechanism: claude-plugin\n  per_repo_required: true\n  binding_prefix: vsdd\n", v)
		if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(py), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("1.0.0-rc.25", filepath.Join(base, "current")); err != nil {
		t.Fatal(err)
	}
	reg := fmt.Sprintf("packs:\n  - name: vsdd-factory\n    version: 1.0.0-rc.25\n    path: %s\n", filepath.Join(base, "current"))
	if err := os.WriteFile(filepath.Join(store, "registry.yaml"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}
	return base
}

func TestResolveStore_PinsANonActiveInstalledVersion(t *testing.T) {
	base := storeWithTwoVersions(t)
	opts := &Options{Pack: "vsdd-factory", Version: "1.0.0-rc.24"}
	root, prefix, perRepo, err := resolveStore(opts)
	if err != nil {
		t.Fatalf("resolveStore(@rc.24) with rc.25 active: %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(base, "1.0.0-rc.24"))
	if root != want {
		t.Errorf("root = %s, want %s", root, want)
	}
	if prefix != "vsdd" || !perRepo {
		t.Errorf("prefix/perRepo = %q/%v, want vsdd/true (read from the pinned version's pack.yaml)", prefix, perRepo)
	}
	if opts.Version != "1.0.0-rc.24" {
		t.Errorf("opts.Version = %s, want 1.0.0-rc.24", opts.Version)
	}
}

func TestResolveStore_PinsTheActiveVersion(t *testing.T) {
	base := storeWithTwoVersions(t)
	root, _, _, err := resolveStore(&Options{Pack: "vsdd-factory", Version: "1.0.0-rc.25"})
	if err != nil {
		t.Fatalf("resolveStore(@rc.25): %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(base, "1.0.0-rc.25"))
	if root != want {
		t.Errorf("root = %s, want %s", root, want)
	}
}

func TestResolveStore_NoVersionUsesActive(t *testing.T) {
	base := storeWithTwoVersions(t)
	opts := &Options{Pack: "vsdd-factory"}
	root, _, _, err := resolveStore(opts)
	if err != nil {
		t.Fatalf("resolveStore(no version): %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(base, "1.0.0-rc.25"))
	if root != want || opts.Version != "1.0.0-rc.25" {
		t.Errorf("root, version = %s, %s; want %s, 1.0.0-rc.25", root, opts.Version, want)
	}
}

func TestResolveStore_UnknownVersionNamesWhatIsInstalled(t *testing.T) {
	storeWithTwoVersions(t)
	_, _, _, err := resolveStore(&Options{Pack: "vsdd-factory", Version: "1.0.0-rc.9"})
	if err == nil {
		t.Fatal("resolveStore(@rc.9) succeeded; want not-installed")
	}
	for _, want := range []string{"vsdd-factory@1.0.0-rc.9 is not installed", "1.0.0-rc.24", "1.0.0-rc.25"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
