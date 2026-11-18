//go:build unix

package pack

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// watchCurrent reads the current link in a tight loop until stop is
// closed and reports the first read that found no link at all.
func watchCurrent(link string, stop <-chan struct{}) (wait func() error) {
	var wg sync.WaitGroup
	var missing atomic.Bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := os.Readlink(link); errors.Is(err, fs.ErrNotExist) {
				missing.Store(true)
				return
			}
		}
	}()
	return func() error {
		wg.Wait()
		if missing.Load() {
			return errors.New("readlink current returned ENOENT during a flip")
		}
		return nil
	}
}

// entryNames lists a pack directory so a test can see what a flip left.
func entryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A reader of the current link never finds it missing while Activate
// moves it between two installed versions.
func TestActivate_CurrentIsNeverMissingDuringAFlip(t *testing.T) {
	freezeSafeHome(t)
	for _, v := range []string{"1.0.0", "2.0.0"} {
		writePackYAML(t, filepath.Join(PacksDir(), "testpack", v), v)
	}
	if err := Activate("testpack", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(PacksDir(), "testpack", "current")

	stop := make(chan struct{})
	wait := watchCurrent(link, stop)
	versions := []string{"2.0.0", "1.0.0"}
	for i := 0; i < 1500; i++ {
		if err := Activate("testpack", versions[i%2]); err != nil {
			close(stop)
			_ = wait()
			t.Fatalf("Activate: %v", err)
		}
	}
	close(stop)
	if err := wait(); err != nil {
		t.Fatal(err)
	}

	want := []string{"1.0.0", "2.0.0", "current"}
	got := entryNames(t, filepath.Join(PacksDir(), "testpack"))
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("pack dir entries after flips = %v, want %v (no temp link left behind)", got, want)
	}
}

// The same holds when Install activates a later version over an
// earlier one.
func TestInstall_CurrentIsNeverMissingDuringAnActivatingInstall(t *testing.T) {
	freezeSafeHome(t)
	mk := func(version string) string {
		src := t.TempDir()
		cfg := filepath.Join(src, "_config")
		if err := os.MkdirAll(cfg, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := "installation:\n  version: \"" + version + "\"\n"
		if err := os.WriteFile(filepath.Join(cfg, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		return src
	}
	if err := InstallFromLocal("testpack", mk("1.0.0"), true); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(PacksDir(), "testpack", "current")

	stop := make(chan struct{})
	wait := watchCurrent(link, stop)
	for i := 2; i < 60; i++ {
		if err := InstallFromLocal("testpack", mk(fmt.Sprintf("%d.0.0", i)), true); err != nil {
			close(stop)
			_ = wait()
			t.Fatalf("install %d: %v", i, err)
		}
	}
	close(stop)
	if err := wait(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(link); err != nil || got != "59.0.0" {
		t.Errorf("current = %q (err %v), want 59.0.0", got, err)
	}
	for _, n := range entryNames(t, filepath.Join(PacksDir(), "testpack")) {
		if len(n) > 0 && n[0] == '.' {
			t.Errorf("temp entry %q left in pack dir", n)
		}
	}
}
