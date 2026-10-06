package pack

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeVersionedPack writes a native pack source at the given version
// with one content file, so two sources can claim the same version and
// differ only in content.
func writeVersionedPack(t *testing.T, version, content string) string {
	t.Helper()
	src := t.TempDir()
	writePackYAML(t, src, version)
	if err := os.WriteFile(filepath.Join(src, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

func readStoreSkill(t *testing.T, version string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(PacksDir(), "testpack", version, "skill.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// aae-orc-mobz8: a second install at an already-installed version, from
// a different tree, must be refused and must leave the store untouched.
// Before the fix it exited 0 and replaced the frozen content in place.
func TestInstallFromLocal_RefusesOverwriteOfInstalledVersion(t *testing.T) {
	freezeSafeHome(t)

	if err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "original"), true); err != nil {
		t.Fatal(err)
	}

	err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "TAMPERED"), true)
	if err == nil {
		t.Fatal("reinstall over an installed version succeeded; want a refusal")
	}
	if got := readStoreSkill(t, "1.0.0"); got != "original" {
		t.Errorf("store content = %q after a refused install, want original", got)
	}
}

// A different version of the same pack is not an overwrite.
func TestInstallFromLocal_NewVersionIsNotAnOverwrite(t *testing.T) {
	freezeSafeHome(t)

	if err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "one"), true); err != nil {
		t.Fatal(err)
	}
	if err := InstallFromLocal("testpack", writeVersionedPack(t, "2.0.0", "two"), true); err != nil {
		t.Fatalf("install of a new version: %v", err)
	}
	if got := readStoreSkill(t, "1.0.0"); got != "one" {
		t.Errorf("1.0.0 content = %q, want one", got)
	}
}

func TestInstall_ForceReplacesInstalledVersion(t *testing.T) {
	freezeSafeHome(t)

	if err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "original"), true); err != nil {
		t.Fatal(err)
	}
	if err := Install("testpack", writeVersionedPack(t, "1.0.0", "replaced"), InstallOptions{Activate: true, Force: true}); err != nil {
		t.Fatalf("forced reinstall: %v", err)
	}
	if got := readStoreSkill(t, "1.0.0"); got != "replaced" {
		t.Errorf("store content = %q after a forced install, want replaced", got)
	}
	// The envelope still refreezes after a forced write.
	if err := os.WriteFile(filepath.Join(PacksDir(), "testpack", "1.0.0", "skill.md"), []byte("x"), 0o644); err == nil {
		t.Error("store version is writable after a forced install; want frozen")
	}
}

func TestInstall_RefusalIsTheSentinelAndStaysFrozen(t *testing.T) {
	freezeSafeHome(t)

	if err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "original"), true); err != nil {
		t.Fatal(err)
	}
	err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "TAMPERED"), true)
	if !errors.Is(err, ErrVersionInstalled) {
		t.Fatalf("err = %v, want ErrVersionInstalled", err)
	}
	if err := os.WriteFile(filepath.Join(PacksDir(), "testpack", "1.0.0", "skill.md"), []byte("x"), 0o644); err == nil {
		t.Error("store version is writable after a refused install; want frozen")
	}
}

// A version directory that exists but holds nothing is not an install:
// the first real install into it must proceed.
func TestInstallFromLocal_EmptyVersionDirIsNotInstalled(t *testing.T) {
	freezeSafeHome(t)

	if err := os.MkdirAll(filepath.Join(PacksDir(), "testpack", "1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "fresh"), true); err != nil {
		t.Fatalf("install into an empty version dir: %v", err)
	}
	if got := readStoreSkill(t, "1.0.0"); got != "fresh" {
		t.Errorf("store content = %q, want fresh", got)
	}
}

// The refusal cannot tell a completed install from one that failed after
// the copy, so it must lead with --force and put `use` last: following
// `use` on a half-failed tree would activate content the install
// rejected. Pins the message against dropping --force or reordering.
func TestInstall_RefusalMessageLeadsWithForce(t *testing.T) {
	freezeSafeHome(t)

	if err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "original"), true); err != nil {
		t.Fatal(err)
	}
	err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "again"), true)
	if err == nil {
		t.Fatal("reinstall succeeded; want a refusal")
	}
	msg := err.Error()
	force := strings.Index(msg, "--force")
	use := strings.Index(msg, "sideshow use")
	if force < 0 {
		t.Fatalf("refusal does not name --force: %q", msg)
	}
	if use >= 0 && use < force {
		t.Errorf("refusal names `sideshow use` before --force: %q", msg)
	}
	if !strings.Contains(msg, "earlier install of this version failed") {
		t.Errorf("refusal does not mention a failed earlier install: %q", msg)
	}
}

// An install that fails after the copy (exec-manifest drift) leaves a
// frozen, non-empty version directory. A retry without Force is refused,
// a forced retry still runs the exec-manifest check, and a forced retry
// from a corrected source recovers.
func TestInstall_HalfFailedInstallNeedsForceAndStillVerifies(t *testing.T) {
	freezeSafeHome(t)
	src := makeModeFixture(t)
	if err := os.Chmod(filepath.Join(src, "bin", "tool.sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "exec-manifest.txt"), []byte("bin/tool.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallFromLocal("modes", src, true); err == nil {
		t.Fatal("install succeeded despite exec-manifest drift")
	}
	if err := InstallFromLocal("modes", src, true); !errors.Is(err, ErrVersionInstalled) {
		t.Fatalf("retry without Force: err = %v, want ErrVersionInstalled", err)
	}
	if err := Install("modes", src, InstallOptions{Activate: true, Force: true}); err == nil {
		t.Fatal("forced install skipped the exec-manifest check; want the drift error")
	}

	if err := os.Chmod(filepath.Join(src, "bin", "tool.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install("modes", src, InstallOptions{Activate: true, Force: true}); err != nil {
		t.Fatalf("forced install from a corrected source: %v", err)
	}
}
