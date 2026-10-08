//go:build unix

package distribute

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func hookScene(t *testing.T, seed string) (repoRoot, settings string) {
	t.Helper()
	repoRoot = t.TempDir()
	settings = filepath.Join(repoRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if seed != "" {
		if err := os.WriteFile(settings, []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repoRoot, settings
}

var testHook = HookArtifact{Event: "SessionStart", Command: "bd prime"}

// A hook merge that cannot complete leaves the previous settings, not a
// torn file (sideshow#183). A directory that cannot take a new file makes
// it observable: an in-place write still succeeds into the file.
func TestDistributeHook_AFailedWriteLeavesThePreviousSettings(t *testing.T) {
	repoRoot, settings := hookScene(t, "{\"env\": {\"A\": \"1\"}}\n")
	before, _ := os.ReadFile(settings)
	dir := filepath.Dir(settings)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	action := distributeHook(repoRoot, testHook, defaultOpts(t.TempDir()))
	if action.Status != "error" {
		t.Errorf("status %q, want error; the settings were written in place", action.Status)
	}
	after, _ := os.ReadFile(settings)
	if string(after) != string(before) {
		t.Errorf("the previous settings changed:\n%q", after)
	}
}

// A hook merge through a symlink leaves the link and updates its target.
func TestDistributeHook_MergeThroughASymlinkUpdatesTheTarget(t *testing.T) {
	repoRoot, settings := hookScene(t, "")
	target := filepath.Join(t.TempDir(), "dotfiles-settings.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, settings); err != nil {
		t.Fatal(err)
	}
	if action := distributeHook(repoRoot, testHook, defaultOpts(t.TempDir())); action.Status != "merged" {
		t.Fatalf("status %q: %s", action.Status, action.Detail)
	}
	if info, err := os.Lstat(settings); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v, %v", info, err)
	}
	if got, _ := os.ReadFile(target); len(got) <= 3 {
		t.Errorf("the target was not updated: %q", got)
	}
}

// A hook merge keeps the mode the file had and leaves no temp file.
func TestDistributeHook_MergeKeepsTheExistingMode(t *testing.T) {
	repoRoot, settings := hookScene(t, "{}\n")
	if err := os.Chmod(settings, 0o600); err != nil {
		t.Fatal(err)
	}
	if action := distributeHook(repoRoot, testHook, defaultOpts(t.TempDir())); action.Status != "merged" {
		t.Fatalf("status %q: %s", action.Status, action.Detail)
	}
	if info, err := os.Stat(settings); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode after merge: %v, %v", info, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(settings)); len(entries) != 1 {
		t.Errorf("stray files after the merge: %v", entries)
	}
}

// A new settings file is 0644 under umask 022.
func TestDistributeHook_NewFileIs0644UnderUmask022(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	repoRoot, settings := hookScene(t, "")
	if action := distributeHook(repoRoot, testHook, defaultOpts(t.TempDir())); action.Status != "merged" {
		t.Fatalf("status %q: %s", action.Status, action.Detail)
	}
	if info, err := os.Stat(settings); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("mode of a new file: %v, %v", info, err)
	}
}
