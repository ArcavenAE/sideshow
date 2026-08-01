package bindings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sideshow#108 regression family: a frozen store source is 0444,
// and served bindings written with that mode verbatim could not be
// overwritten by the next sync, so every other version flip removed
// everything and synced nothing.

func TestWriteWithSourceMode_KeepsOwnerWriteFromFrozenSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.md")
	if err := os.WriteFile(src, []byte("v1"), 0o444); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "served.md")
	if err := writeWithSourceMode(target, []byte("v1"), src); err != nil {
		t.Fatalf("first write: %v", err)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o200 == 0 {
		t.Fatalf("served binding lost the owner write bit: %v", fi.Mode())
	}
	// The second write is the one that failed before the fix.
	if err := writeWithSourceMode(target, []byte("v2"), src); err != nil {
		t.Fatalf("overwrite own output: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("content = %q, want v2", got)
	}
}

func TestWriteWithSourceMode_KeepsExecBit(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tool.sh")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o555); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "served.sh")
	if err := writeWithSourceMode(target, []byte("#!/bin/sh\n"), src); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("exec bit lost: %v", fi.Mode())
	}
	if fi.Mode().Perm()&0o200 == 0 {
		t.Fatalf("owner write bit lost: %v", fi.Mode())
	}
}

func TestWriteWithSourceMode_UnlocksPreFixReadOnlyTarget(t *testing.T) {
	// Machines that synced from a frozen store before the fix carry
	// 0444 served bindings; the writer must self-heal them in place.
	dir := t.TempDir()
	src := filepath.Join(dir, "src.md")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "served.md")
	if err := os.WriteFile(target, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := writeWithSourceMode(target, []byte("new"), src); err != nil {
		t.Fatalf("overwrite pre-fix read-only target: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
}

// fakeBinding drives runSync without a real store.
type fakeBinding struct {
	name    string
	syncErr error
	arts    []string
}

func (f *fakeBinding) Kind() string                 { return "fake" }
func (f *fakeBinding) PackName() string             { return f.name }
func (f *fakeBinding) PackVersion() string          { return "1.0.0" }
func (f *fakeBinding) Validate() error              { return nil }
func (f *fakeBinding) Artifacts() ([]string, error) { return f.arts, nil }
func (f *fakeBinding) Sync() (int, error) {
	if f.syncErr != nil {
		return 0, f.syncErr
	}
	return len(f.arts), nil
}

func TestRunSync_FailureSkipsReconcileAndErrors(t *testing.T) {
	// Isolate the manifest under a temp home.
	t.Setenv("SIDESHOW_HOME", t.TempDir())
	served := filepath.Join(t.TempDir(), "kept.md")
	if err := os.WriteFile(served, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Prior manifest says the failing binding owns served content.
	if err := saveManifest([]ManifestEntry{{Pack: "alpha", Version: "1.0.0", Kind: "fake", Path: served}}); err != nil {
		t.Fatal(err)
	}

	synced, removed, err := runSync([]Binding{
		&fakeBinding{name: "alpha", syncErr: errors.New("permission denied")},
		&fakeBinding{name: "beta", arts: []string{filepath.Join(t.TempDir(), "b.md")}},
	})
	if err == nil {
		t.Fatal("a failed binding must make the sync fail loudly, not exit clean")
	}
	if !strings.Contains(err.Error(), "1 binding(s) failed") {
		t.Errorf("error must count failures: %v", err)
	}
	if removed != 0 {
		t.Errorf("reconcile must be skipped on failure, removed = %d", removed)
	}
	if synced != 1 {
		t.Errorf("healthy bindings still sync, got %d", synced)
	}
	if _, statErr := os.Stat(served); statErr != nil {
		t.Errorf("the failed binding's served artifact was removed: %v", statErr)
	}
}

func TestRunSync_CleanPathStillReconciles(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())
	art := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(art, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	synced, _, err := runSync([]Binding{&fakeBinding{name: "alpha", arts: []string{art}}})
	if err != nil {
		t.Fatalf("clean sync: %v", err)
	}
	if synced != 1 {
		t.Errorf("synced = %d, want 1", synced)
	}
}
