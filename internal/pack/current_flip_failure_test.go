//go:build unix

package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Where current is a directory, not a link, the flip refuses: a rename of
// a link over a directory fails, and the old behavior of removing an empty
// directory first and then linking is gone. The failed rename leaves the
// directory and the version dirs as they were, and no temp link behind.
func TestActivate_RefusesWhenCurrentIsADirectory(t *testing.T) {
	for name, withFile := range map[string]bool{"empty directory": false, "non-empty directory": true} {
		t.Run(name, func(t *testing.T) {
			freezeSafeHome(t)
			writePackYAML(t, filepath.Join(PacksDir(), "testpack", "1.0.0"), "1.0.0")
			current := filepath.Join(PacksDir(), "testpack", "current")
			if err := os.Mkdir(current, 0o755); err != nil {
				t.Fatal(err)
			}
			if withFile {
				if err := os.WriteFile(filepath.Join(current, "keep"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			err := Activate("testpack", "1.0.0")
			if err == nil {
				t.Fatal("Activate replaced a current that is a directory, want an error")
			}
			if !strings.Contains(err.Error(), "current symlink") {
				t.Errorf("error %q does not name the current symlink", err)
			}
			info, statErr := os.Lstat(current)
			if statErr != nil || !info.IsDir() {
				t.Errorf("current after the refusal: %v (err %v), want the directory untouched", info, statErr)
			}
			for _, n := range entryNames(t, filepath.Join(PacksDir(), "testpack")) {
				if strings.HasPrefix(n, ".current.") {
					t.Errorf("temp link %q left behind after the failed rename", n)
				}
			}
			if reg, err := LoadRegistry(); err != nil || len(reg.Packs) != 0 {
				t.Errorf("registry after the refused flip = %+v (err %v), want no pack recorded", reg, err)
			}
		})
	}
}

// The same refusal through an activating Install, which must not record
// the version in the registry when the flip fails.
func TestInstall_FailedFlipLeavesNoTempLinkAndNoRegistryRow(t *testing.T) {
	freezeSafeHome(t)
	if err := os.MkdirAll(filepath.Join(PacksDir(), "testpack", "current"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	cfg := filepath.Join(src, "_config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "manifest.yaml"), []byte("installation:\n  version: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallFromLocal("testpack", src, true); err == nil {
		t.Fatal("activating Install replaced a current that is a directory, want an error")
	}
	for _, n := range entryNames(t, filepath.Join(PacksDir(), "testpack")) {
		if strings.HasPrefix(n, ".current.") {
			t.Errorf("temp link %q left behind after the failed rename", n)
		}
	}
	if reg, err := LoadRegistry(); err != nil || len(reg.Packs) != 0 {
		t.Errorf("registry after the failed flip = %+v (err %v), want no pack recorded", reg, err)
	}
}
