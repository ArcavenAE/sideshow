//go:build unix

package distribute

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// overwriteScene lays out one writer with a file already on disk holding
// seed, and returns the file's path, the seed, and a save that changes it.
type overwriteScene func(t *testing.T) (path, seed string, save func() error)

func actionErr(a Action) error {
	if a.Status == "error" {
		return errors.New(a.Detail)
	}
	return nil
}

// overwritePins runs the four pins ruled for sideshow#185 against a writer
// that replaces an existing file.
func overwritePins(t *testing.T, scene overwriteScene) {
	t.Run("AFailedWriteLeavesThePreviousBytes", func(t *testing.T) {
		path, seed, save := scene(t)
		dir := filepath.Dir(path)
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		if err := save(); err == nil {
			t.Fatal("a write into a directory that cannot take a new file succeeded; the file was written in place")
		}
		if got, _ := os.ReadFile(path); string(got) != seed {
			t.Errorf("the previous bytes changed: %q", got)
		}
	})
	t.Run("WriteThroughASymlinkUpdatesTheTarget", func(t *testing.T) {
		path, seed, save := scene(t)
		target := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if err := save(); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the link was replaced: %v, %v", info, err)
		}
		if got, _ := os.ReadFile(target); string(got) == seed {
			t.Errorf("the target was not updated")
		}
	})
	t.Run("WriteKeepsTheExistingMode", func(t *testing.T) {
		path, _, save := scene(t)
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := save(); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("mode after the write: %v, %v", info, err)
		}
		if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
			t.Errorf("stray files after the write: %v", entries)
		}
	})
	t.Run("NewFileIs0644UnderUmask022", func(t *testing.T) {
		old := syscall.Umask(0o022)
		t.Cleanup(func() { syscall.Umask(old) })
		path, _, save := scene(t)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := save(); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
			t.Errorf("mode of a new file: %v, %v", info, err)
		}
	})
}

func packFile(t *testing.T, name, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func plant(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDistributeRule_AtomicPins(t *testing.T) {
	overwritePins(t, func(t *testing.T) (string, string, func() error) {
		repo := t.TempDir()
		packRoot := packFile(t, "rule.md", "new rule\n")
		path := filepath.Join(repo, ".claude", "rules", "r.md")
		seed := fileMarker("testpack", "0.9.0") + "\nold rule\n"
		plant(t, path, seed)
		opts := defaultOpts(packRoot)
		// The receipt says sideshow wrote these bytes, so an update is allowed.
		opts.PriorRuleChecksums = map[string]string{".claude/rules/r.md": "sha256:" + sha256hex([]byte(seed))}
		return path, seed, func() error {
			return actionErr(distributeRule(repo, RuleArtifact{Source: "rule.md", Target: ".claude/rules/r.md"}, opts))
		}
	})
}

func TestDistributeClaudeMD_AtomicPins(t *testing.T) {
	overwritePins(t, func(t *testing.T) (string, string, func() error) {
		repo := t.TempDir()
		packRoot := packFile(t, "section.md", "section body\n")
		path := filepath.Join(repo, "CLAUDE.md")
		seed := "# Mine\n"
		plant(t, path, seed)
		return path, seed, func() error {
			return actionErr(distributeClaudeMD(repo, ClaudeMDArtifact{ID: "s1", Source: "section.md"}, defaultOpts(packRoot)))
		}
	})
}

func TestDistributeGitignore_AtomicPins(t *testing.T) {
	overwritePins(t, func(t *testing.T) (string, string, func() error) {
		repo := t.TempDir()
		path := filepath.Join(repo, ".gitignore")
		seed := "node_modules\n"
		plant(t, path, seed)
		return path, seed, func() error {
			return actionErr(distributeGitignore(repo, "/_x/shim", defaultOpts(t.TempDir())))
		}
	})
}

func TestDistributeFile_AtomicPins(t *testing.T) {
	overwritePins(t, func(t *testing.T) (string, string, func() error) {
		repo := t.TempDir()
		packRoot := packFile(t, "cfg", "new bytes\n")
		path := filepath.Join(repo, "conf", "x.cfg")
		seed := "old bytes\n"
		plant(t, path, seed)
		opts := defaultOpts(packRoot)
		opts.PriorChecksums = map[string]string{"conf/x.cfg": "sha256:" + sha256hex([]byte(seed))}
		return path, seed, func() error {
			return actionErr(distributeFile(repo, FileArtifact{Source: "cfg", Target: "conf/x.cfg"}, opts))
		}
	})
}

// The seed writes into a per-repo dir that distribute just created and
// never touches again, so a torn seed would stay torn. It never overwrites,
// so there is no previous-bytes pin: a new file is 0644 under umask 022,
// leaves no temp file, and a dangling link at the path gets its target.
func TestSeedCustomTemplate_AtomicPins(t *testing.T) {
	scene := func(t *testing.T) (templateDir, destDir string) {
		templateDir = t.TempDir()
		plant(t, filepath.Join(templateDir, "a.toml"), "seed\n")
		destDir = filepath.Join(t.TempDir(), "dest")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatal(err)
		}
		return templateDir, destDir
	}
	t.Run("NewFileIs0644UnderUmask022AndLeavesNoTempFile", func(t *testing.T) {
		old := syscall.Umask(0o022)
		t.Cleanup(func() { syscall.Umask(old) })
		tpl, dest := scene(t)
		if n, err := seedCustomTemplate(tpl, dest); err != nil || n != 1 {
			t.Fatalf("seed: %d, %v", n, err)
		}
		if info, err := os.Stat(filepath.Join(dest, "a.toml")); err != nil || info.Mode().Perm() != 0o644 {
			t.Errorf("mode of a new file: %v, %v", info, err)
		}
		if entries, _ := os.ReadDir(dest); len(entries) != 1 {
			t.Errorf("stray files beside the seed: %v", entries)
		}
	})
	t.Run("DanglingSymlinkCreatesItsTarget", func(t *testing.T) {
		tpl, dest := scene(t)
		target := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.Symlink(target, filepath.Join(dest, "a.toml")); err != nil {
			t.Fatal(err)
		}
		if _, err := seedCustomTemplate(tpl, dest); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Lstat(filepath.Join(dest, "a.toml")); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the link was replaced: %v, %v", info, err)
		}
		if _, err := os.Stat(target); err != nil {
			t.Errorf("the target was not created: %v", err)
		}
	})
}
