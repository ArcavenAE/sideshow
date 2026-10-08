package bindings

import (
	"os"
	"path/filepath"
	"testing"
)

func unreadable(t *testing.T, path string) {
	t.Helper()
	writeFile(t, path, "x")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
}

func cmdPack(t *testing.T, files map[string]string) string {
	t.Helper()
	p := t.TempDir()
	for name, text := range files {
		writeFile(t, filepath.Join(p, "commands", name), text)
	}
	return p
}

func recordedPacks(t *testing.T) map[string][]string {
	t.Helper()
	m, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, e := range m.Entries {
		out[filepath.Base(e.Path)] = append(out[filepath.Base(e.Path)], e.Pack)
	}
	return out
}

// A binding that fails after writing a shared path still owns its bytes:
// the manifest names it, not the pack whose copy it overwrote
// (sideshow#161, review of #170).
func TestRunSync_FailedBindingIsRecordedForWhatItWrote(t *testing.T) {
	collisionEnv(t)
	a := cmdPack(t, map[string]string{"shared.md": "alpha"})
	b := cmdPack(t, map[string]string{"shared.md": "beta"})
	unreadable(t, filepath.Join(b, "commands", "zzz.md"))

	if _, _, err := runSync([]Binding{NewMarkdownCommandBinding("alpha", "1", a)}); err != nil {
		t.Fatal(err)
	}
	var err error
	captureStderr(t, func() {
		_, _, err = runSync([]Binding{NewMarkdownCommandBinding("beta", "1", b)})
	})
	if err == nil {
		t.Fatal("failing binding exited zero")
	}
	got := recordedPacks(t)
	if len(got["shared.md"]) != 1 || got["shared.md"][0] != "beta" {
		t.Errorf("shared.md recorded for %v, want only beta (its bytes are on disk)", got["shared.md"])
	}
	if _, ok := got["zzz.md"]; ok {
		t.Error("zzz.md was never written but has an entry")
	}
	m, _ := LoadManifest()
	if m.IsComplete() || len(m.Failed) != 1 || m.Failed[0].Pack != "beta" {
		t.Errorf("complete=%v failed=%+v, want incomplete naming beta", m.IsComplete(), m.Failed)
	}
}

// The converse: beta fails before it writes shared.md, so alpha's entry
// stays and beta has none for either file.
func TestRunSync_BindingFailingBeforeItWritesLeavesThePreviousWriter(t *testing.T) {
	collisionEnv(t)
	a := cmdPack(t, map[string]string{"shared.md": "alpha"})
	b := cmdPack(t, map[string]string{"shared.md": "beta"})
	unreadable(t, filepath.Join(b, "commands", "aaa.md"))

	if _, _, err := runSync([]Binding{NewMarkdownCommandBinding("alpha", "1", a)}); err != nil {
		t.Fatal(err)
	}
	captureStderr(t, func() {
		_, _, _ = runSync([]Binding{NewMarkdownCommandBinding("beta", "1", b)})
	})
	got := recordedPacks(t)
	if len(got["shared.md"]) != 1 || got["shared.md"][0] != "alpha" {
		t.Errorf("shared.md recorded for %v, want only alpha", got["shared.md"])
	}
	if _, ok := got["aaa.md"]; ok {
		t.Error("aaa.md was never written but has an entry")
	}
}

// Each Sync returns the paths that landed before the failure, in write
// order, and the error.
func TestSync_FailureOnTheNthFileReturnsTheFirstNMinusOne(t *testing.T) {
	t.Run("markdown-command", func(t *testing.T) {
		home := collisionEnv(t)
		p := cmdPack(t, map[string]string{"a.md": "a", "b.md": "b"})
		unreadable(t, filepath.Join(p, "commands", "c.md"))
		_, written, err := NewMarkdownCommandBinding("p", "1", p).Sync()
		if err == nil {
			t.Fatal("no error")
		}
		want := []string{filepath.Join(home, ".claude", "commands", "a.md"), filepath.Join(home, ".claude", "commands", "b.md")}
		assertPaths(t, written, want)
	})
	t.Run("skill-dir", func(t *testing.T) {
		home := collisionEnv(t)
		p := t.TempDir()
		writeFile(t, filepath.Join(p, ".claude", "skills", "s1", "SKILL.md"), "1")
		writeFile(t, filepath.Join(p, ".claude", "skills", "s2", "SKILL.md"), "2")
		unreadable(t, filepath.Join(p, ".claude", "skills", "s3", "SKILL.md"))
		_, written, err := NewSkillDirBinding("p", "1", p).Sync()
		if err == nil {
			t.Fatal("no error")
		}
		sk := filepath.Join(home, ".claude", "skills")
		assertPaths(t, written, []string{filepath.Join(sk, "s1"), filepath.Join(sk, "s2")})
	})
	t.Run("custom-skill-dir", func(t *testing.T) {
		home := collisionEnv(t)
		project := t.TempDir()
		base := filepath.Join(project, "_p-custom", "skills")
		writeFile(t, filepath.Join(base, "s1", "SKILL.md"), "1")
		writeFile(t, filepath.Join(base, "s2", "SKILL.md"), "2")
		unreadable(t, filepath.Join(base, "s3", "SKILL.md"))
		_, written, err := NewCustomSkillDirBinding("p", project, []string{"s1", "s2", "s3"}).Sync()
		if err == nil {
			t.Fatal("no error")
		}
		sk := filepath.Join(home, ".claude", "skills")
		assertPaths(t, written, []string{filepath.Join(sk, "s1"), filepath.Join(sk, "s2")})
	})
}

func assertPaths(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("written = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("written[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// A skill directory is the unit of ownership: once one file in it has
// been written its bytes are on disk, so it is reported and recorded even
// when a later file in the same skill fails. A skill whose first file
// fails wrote nothing and gets no entry (sideshow#161).
func TestSync_PartialSkillDirIsReportedAndRecorded(t *testing.T) {
	t.Run("skill-dir", func(t *testing.T) {
		home := collisionEnv(t)
		p := t.TempDir()
		writeFile(t, filepath.Join(p, ".claude", "skills", "a-part", "SKILL.md"), "ok")
		unreadable(t, filepath.Join(p, ".claude", "skills", "a-part", "zz.md"))
		q := t.TempDir()
		unreadable(t, filepath.Join(q, ".claude", "skills", "z-none", "AAA.md"))
		writeFile(t, filepath.Join(q, ".claude", "skills", "z-none", "SKILL.md"), "never")

		var err error
		captureStderr(t, func() {
			_, _, err = runSync([]Binding{NewSkillDirBinding("beta", "1", p), NewSkillDirBinding("gamma", "1", q)})
		})
		if err == nil {
			t.Fatal("no error")
		}
		got := recordedPacks(t)
		if len(got["a-part"]) != 1 || got["a-part"][0] != "beta" {
			t.Errorf("a-part recorded for %v, want beta (SKILL.md landed)", got["a-part"])
		}
		if _, ok := got["z-none"]; ok {
			t.Error("a skill whose first file failed has an entry")
		}
		_, written, _ := NewSkillDirBinding("beta", "1", p).Sync()
		assertPaths(t, written, []string{filepath.Join(home, ".claude", "skills", "a-part")})
	})
	t.Run("custom-skill-dir", func(t *testing.T) {
		home := collisionEnv(t)
		project := t.TempDir()
		base := filepath.Join(project, "_p-custom", "skills")
		writeFile(t, filepath.Join(base, "part", "SKILL.md"), "ok")
		unreadable(t, filepath.Join(base, "part", "zz.md"))
		unreadable(t, filepath.Join(base, "none", "AAA.md"))
		writeFile(t, filepath.Join(base, "none", "SKILL.md"), "never")

		_, written, err := NewCustomSkillDirBinding("p", project, []string{"part", "none"}).Sync()
		if err == nil {
			t.Fatal("no error")
		}
		assertPaths(t, written, []string{filepath.Join(home, ".claude", "skills", "part")})

		// "none" fails first in its own tree and gets no entry.
		_, written, err = NewCustomSkillDirBinding("p", project, []string{"none"}).Sync()
		if err == nil {
			t.Fatal("no error")
		}
		assertPaths(t, written, nil)
	})
}
