package bindings

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepoFixtureStore builds a resolved store version root carrying
// two skills (one with an executable helper), a nested agent layout,
// and an exec-manifest census.
func writeRepoFixtureStore(t *testing.T) string {
	t.Helper()
	store := t.TempDir()

	mustWrite := func(rel, content string, mode os.FileMode) {
		t.Helper()
		p := filepath.Join(store, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("skills/pr-manager/SKILL.md", "# pr-manager\n", 0o644)
	mustWrite("skills/pr-manager/bin/helper.sh", "#!/bin/sh\necho ok\n", 0o755)
	mustWrite("skills/adversary/SKILL.md", "# adversary\n", 0o644)
	mustWrite("agents/reviewer.md", "---\nname: reviewer\n---\n", 0o644)
	mustWrite("agents/orchestrator/planner.md", "---\nname: planner\n---\n", 0o644)
	mustWrite("exec-manifest.txt", "skills/pr-manager/bin/helper.sh\n", 0o644)

	return store
}

func fixtureInventory() *PluginInventory {
	return &PluginInventory{
		SkillDirs:  []string{"skills/adversary", "skills/pr-manager"},
		AgentFiles: []string{"agents/orchestrator/planner.md", "agents/reviewer.md"},
	}
}

// writeRepoWithUserContent builds a repo that already carries user
// content the materialization must neither touch nor remove.
func writeRepoWithUserContent(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for rel, content := range map[string]string{
		".claude/settings.json":        `{"user": true}` + "\n",
		".claude/skills/mine/SKILL.md": "# mine\n",
		"README.md":                    "user repo\n",
	} {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

// snapshotTree fingerprints every entry under dir: type, perm, and
// content hash (or symlink target), keyed by relative path.
func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snap[rel] = "link -> " + target
		case info.IsDir():
			snap[rel] = "dir"
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snap[rel] = fmt.Sprintf("file %o %x", info.Mode().Perm(), sha256.Sum256(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func diffSnapshots(before, after map[string]string) []string {
	var out []string
	for k, v := range after {
		if bv, ok := before[k]; !ok {
			out = append(out, "added "+k)
		} else if bv != v {
			out = append(out, "changed "+k)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			out = append(out, "removed "+k)
		}
	}
	return out
}

// The .48 done criterion: enable and disable are exact inverses on a
// repo containing pre-existing user content, at both D1 scopes.
func TestMaterializeThenRemove_ExactInverse(t *testing.T) {
	t.Parallel()
	for _, scope := range []RepoScope{ScopeLocal, ScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			t.Parallel()
			store := writeRepoFixtureStore(t)
			repo := writeRepoWithUserContent(t)
			target := RepoTarget{RepoDir: repo, Scope: scope}

			before := snapshotTree(t, repo)

			artifacts, err := MaterializeRepo(store, fixtureInventory(), target)
			if err != nil {
				t.Fatalf("MaterializeRepo: %v", err)
			}
			if len(artifacts) == 0 {
				t.Fatal("no artifacts recorded")
			}

			if _, err := RemoveRepoArtifacts(target, artifacts); err != nil {
				t.Fatalf("RemoveRepoArtifacts: %v", err)
			}

			after := snapshotTree(t, repo)
			if diff := diffSnapshots(before, after); len(diff) > 0 {
				t.Errorf("repo not restored exactly:\n  %s", strings.Join(diff, "\n  "))
			}
		})
	}
}

// Every write lands inside <repo>/.claude — nothing else in the repo
// changes on enable.
func TestMaterializeRepo_WritesOnlyUnderBindingRoot(t *testing.T) {
	t.Parallel()
	store := writeRepoFixtureStore(t)
	repo := writeRepoWithUserContent(t)
	target := RepoTarget{RepoDir: repo, Scope: ScopeLocal}

	before := snapshotTree(t, repo)
	artifacts, err := MaterializeRepo(store, fixtureInventory(), target)
	if err != nil {
		t.Fatalf("MaterializeRepo: %v", err)
	}
	after := snapshotTree(t, repo)

	for _, d := range diffSnapshots(before, after) {
		if !strings.HasPrefix(d, "added .claude/") {
			t.Errorf("write outside binding root: %s", d)
		}
	}
	for _, a := range artifacts {
		if a.Path != ".claude" && !strings.HasPrefix(a.Path, ".claude/") {
			t.Errorf("artifact outside binding root: %+v", a)
		}
	}
}

func TestMaterializeRepo_LocalScopeSymlinksToStore(t *testing.T) {
	t.Parallel()
	store := writeRepoFixtureStore(t)
	repo := t.TempDir()

	if _, err := MaterializeRepo(store, fixtureInventory(), RepoTarget{RepoDir: repo, Scope: ScopeLocal}); err != nil {
		t.Fatalf("MaterializeRepo: %v", err)
	}

	link := filepath.Join(repo, ".claude", "skills", "pr-manager")
	targetPath, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("skill unit is not a symlink: %v", err)
	}
	if want := filepath.Join(store, "skills", "pr-manager"); targetPath != want {
		t.Errorf("symlink target = %s, want %s", targetPath, want)
	}
	// The store copy stays the only copy carrying exec bits.
	if _, err := os.Stat(filepath.Join(link, "bin", "helper.sh")); err != nil {
		t.Errorf("symlinked skill content unreachable: %v", err)
	}
	if _, err := os.Readlink(filepath.Join(repo, ".claude", "agents", "orchestrator", "planner.md")); err != nil {
		t.Errorf("nested agent file is not a symlink: %v", err)
	}
}

func TestMaterializeRepo_ProjectScopeCopiesWithExecBits(t *testing.T) {
	t.Parallel()
	store := writeRepoFixtureStore(t)
	repo := t.TempDir()

	if _, err := MaterializeRepo(store, fixtureInventory(), RepoTarget{RepoDir: repo, Scope: ScopeProject}); err != nil {
		t.Fatalf("MaterializeRepo: %v", err)
	}

	helper := filepath.Join(repo, ".claude", "skills", "pr-manager", "bin", "helper.sh")
	info, err := os.Lstat(helper)
	if err != nil {
		t.Fatalf("copied helper missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("project scope produced a symlink; committed bindings must be self-contained bytes")
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("copied executable lost its exec bit: mode %o", info.Mode().Perm())
	}
	if info, err := os.Lstat(filepath.Join(repo, ".claude", "agents", "reviewer.md")); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("agent file should be a regular copy (err=%v)", err)
	}
}

// Clobber protection: a pre-existing destination refuses the whole
// materialization before anything is written.
func TestMaterializeRepo_RefusesClobber(t *testing.T) {
	t.Parallel()
	store := writeRepoFixtureStore(t)
	repo := t.TempDir()
	pre := filepath.Join(repo, ".claude", "skills", "pr-manager")
	if err := os.MkdirAll(pre, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pre, "SKILL.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := snapshotTree(t, repo)
	_, err := MaterializeRepo(store, fixtureInventory(), RepoTarget{RepoDir: repo, Scope: ScopeLocal})
	if !errors.Is(err, ErrWouldClobber) {
		t.Fatalf("err = %v, want ErrWouldClobber", err)
	}
	if diff := diffSnapshots(before, snapshotTree(t, repo)); len(diff) > 0 {
		t.Errorf("refused materialization still wrote:\n  %s", strings.Join(diff, "\n  "))
	}
}

// The repo-rooted containment predicate: recorded paths outside
// <repo>/.claude fail closed instead of being removed.
func TestRemoveRepoArtifacts_ContainmentFailsClosed(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(repo, "README.md")
	if err := os.WriteFile(victim, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := RepoTarget{RepoDir: repo, Scope: ScopeLocal}

	for _, bad := range []RepoArtifact{
		{Kind: ArtifactAgentFile, Path: "README.md"},
		{Kind: ArtifactAgentFile, Path: ".claude/../README.md"},
		{Kind: ArtifactSkillDir, Path: ".claude"},
		{Kind: ArtifactAgentFile, Path: victim},
	} {
		if _, err := RemoveRepoArtifacts(target, []RepoArtifact{bad}); err == nil {
			t.Errorf("artifact %+v: removal accepted, want refusal", bad)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("containment breach: README.md removed")
	}
}

// Parent dirs created by enable are pruned only when empty; user
// content that arrived after enable survives disable.
func TestRemoveRepoArtifacts_SparesUserContentInParents(t *testing.T) {
	t.Parallel()
	store := writeRepoFixtureStore(t)
	repo := t.TempDir()
	target := RepoTarget{RepoDir: repo, Scope: ScopeLocal}

	artifacts, err := MaterializeRepo(store, fixtureInventory(), target)
	if err != nil {
		t.Fatalf("MaterializeRepo: %v", err)
	}

	// User adds a skill of their own after enable.
	mine := filepath.Join(repo, ".claude", "skills", "mine")
	if err := os.MkdirAll(mine, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveRepoArtifacts(target, artifacts); err != nil {
		t.Fatalf("RemoveRepoArtifacts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mine, "SKILL.md")); err != nil {
		t.Errorf("user skill removed by disable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude", "skills", "pr-manager")); !os.IsNotExist(err) {
		t.Errorf("materialized skill survived disable (err=%v)", err)
	}
	// agents/ had no user content, so it is pruned entirely.
	if _, err := os.Stat(filepath.Join(repo, ".claude", "agents")); !os.IsNotExist(err) {
		t.Errorf("empty created agents dir not pruned (err=%v)", err)
	}
}

// Validation rule 3: a census entry that lands non-executable fails the
// project-scope materialization (and rolls it back).
func TestMaterializeRepo_ExecCensusViolationRollsBack(t *testing.T) {
	t.Parallel()
	store := writeRepoFixtureStore(t)
	// Break the store: census names the helper, store copy lost its bit.
	if err := os.Chmod(filepath.Join(store, "skills", "pr-manager", "bin", "helper.sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()

	before := snapshotTree(t, repo)
	_, err := MaterializeRepo(store, fixtureInventory(), RepoTarget{RepoDir: repo, Scope: ScopeProject})
	if err == nil || !strings.Contains(err.Error(), execManifestName) {
		t.Fatalf("err = %v, want exec-manifest verification failure", err)
	}
	if diff := diffSnapshots(before, snapshotTree(t, repo)); len(diff) > 0 {
		t.Errorf("failed materialization left residue:\n  %s", strings.Join(diff, "\n  "))
	}
}

func TestMaterializeRepo_HarnessDirIsAParameter(t *testing.T) {
	t.Parallel()
	store := writeRepoFixtureStore(t)
	repo := t.TempDir()
	target := RepoTarget{RepoDir: repo, HarnessDir: ".agents", Scope: ScopeLocal}

	artifacts, err := MaterializeRepo(store, fixtureInventory(), target)
	if err != nil {
		t.Fatalf("MaterializeRepo: %v", err)
	}
	if _, err := os.Readlink(filepath.Join(repo, ".agents", "skills", "pr-manager")); err != nil {
		t.Fatalf("binding not under parameterized harness dir: %v", err)
	}
	if _, err := RemoveRepoArtifacts(target, artifacts); err != nil {
		t.Fatalf("RemoveRepoArtifacts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents")); !os.IsNotExist(err) {
		t.Errorf("parameterized harness dir not pruned (err=%v)", err)
	}
}
