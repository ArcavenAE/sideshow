package bindings

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr returns what fn wrote to stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stderr = old
	return <-done
}

func collisionEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("SIDESHOW_HOME", t.TempDir())
	return home
}

func skillPack(t *testing.T, id, text string) string {
	t.Helper()
	p := t.TempDir()
	writeFile(t, filepath.Join(p, ".claude", "skills", id, "SKILL.md"), text)
	return p
}

// Two packs in one sync that ship the same id: the later write wins, as
// before, and the sync says so instead of staying silent (aae-orc-em8e).
func TestRunSync_WarnsWhenTwoPacksWriteTheSamePath(t *testing.T) {
	home := collisionEnv(t)
	a := skillPack(t, "shared-skill", "alpha text")
	b := skillPack(t, "shared-skill", "beta text")

	var n int
	var err error
	out := captureStderr(t, func() {
		n, _, err = runSync([]Binding{NewSkillDirBinding("alpha", "1", a), NewSkillDirBinding("beta", "1", b)})
	})
	if err != nil {
		t.Fatalf("runSync: %v", err)
	}
	if n != 2 {
		t.Errorf("synced = %d, want 2 (writes unchanged)", n)
	}
	for _, want := range []string{"shared-skill", "alpha", "beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "warning:") {
		t.Errorf("not a warning line:\n%s", out)
	}
	got, _ := os.ReadFile(filepath.Join(home, ".claude", "skills", "shared-skill", "SKILL.md"))
	if !strings.HasPrefix(string(got), "beta text") {
		t.Errorf("last writer should still win; disk holds %q", got)
	}
}

// A later sync of one pack over a path the previous sync recorded for
// another pack warns too.
func TestRunSync_WarnsWhenOverwritingAnotherPacksRecordedPath(t *testing.T) {
	collisionEnv(t)
	a := skillPack(t, "shared-skill", "alpha text")
	b := skillPack(t, "shared-skill", "beta text")

	if _, _, err := runSync([]Binding{NewSkillDirBinding("alpha", "1", a)}); err != nil {
		t.Fatal(err)
	}
	var err error
	out := captureStderr(t, func() {
		_, _, err = runSync([]Binding{NewSkillDirBinding("beta", "1", b)})
	})
	if err != nil {
		t.Fatalf("runSync: %v", err)
	}
	for _, want := range []string{"shared-skill", "alpha", "beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning lacks %q:\n%s", want, out)
		}
	}
}

// Distinct ids, and one pack syncing over its own earlier record (a new
// version, or a second run), warn about nothing.
func TestRunSync_NoWarningWithoutACollision(t *testing.T) {
	collisionEnv(t)
	a := skillPack(t, "alpha-skill", "alpha text")
	b := skillPack(t, "beta-skill", "beta text")

	out := captureStderr(t, func() {
		for i := 0; i < 2; i++ {
			if _, _, err := runSync([]Binding{NewSkillDirBinding("alpha", "1", a), NewSkillDirBinding("beta", "1", b)}); err != nil {
				t.Errorf("runSync: %v", err)
			}
		}
		if _, _, err := runSync([]Binding{NewSkillDirBinding("alpha", "2", a), NewSkillDirBinding("beta", "1", b)}); err != nil {
			t.Errorf("runSync: %v", err)
		}
	})
	if strings.Contains(out, "overwrit") || strings.Contains(out, "collision") {
		t.Errorf("collision warning without a collision:\n%s", out)
	}
}

// A collision in the previous manifest and again now names the path once
// per sync, not once per pack.
func TestRunSync_WarnsOncePerPath(t *testing.T) {
	collisionEnv(t)
	a := skillPack(t, "shared-skill", "alpha text")
	b := skillPack(t, "shared-skill", "beta text")
	bs := []Binding{NewSkillDirBinding("alpha", "1", a), NewSkillDirBinding("beta", "1", b)}
	if _, _, err := runSync(bs); err != nil {
		t.Fatal(err)
	}
	out := captureStderr(t, func() {
		if _, _, err := runSync(bs); err != nil {
			t.Errorf("runSync: %v", err)
		}
	})
	if c := strings.Count(out, "shared-skill"); c != 1 {
		t.Errorf("path named %d times, want once:\n%s", c, out)
	}
}

// One pack with two bindings that list the same destination (a pack
// source plus a custom source for the same pack) is not a collision
// between packs.
func TestRunSync_SamePackTwoBindingsOnOnePathIsNotACollision(t *testing.T) {
	collisionEnv(t)
	a1 := skillPack(t, "shared-skill", "first text")
	a2 := skillPack(t, "shared-skill", "second text")
	out := captureStderr(t, func() {
		if _, _, err := runSync([]Binding{NewSkillDirBinding("alpha", "1", a1), NewSkillDirBinding("alpha", "1", a2)}); err != nil {
			t.Errorf("runSync: %v", err)
		}
	})
	if strings.Contains(out, "written by") || strings.Contains(out, "overwrites") {
		t.Errorf("one pack reported as colliding with itself:\n%s", out)
	}
}
