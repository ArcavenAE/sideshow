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
// closed. started closes after the first read, so a test can begin its
// flips only once the reader is running. wait reports the number of reads
// and an error if any read found no link at all.
func watchCurrent(link string, stop <-chan struct{}) (started <-chan struct{}, wait func() (int64, error)) {
	var wg sync.WaitGroup
	var missing atomic.Bool
	var reads atomic.Int64
	first := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, err := os.Readlink(link)
			if reads.Add(1) == 1 {
				close(first)
			}
			if errors.Is(err, fs.ErrNotExist) {
				missing.Store(true)
				return
			}
		}
	}()
	return first, func() (int64, error) {
		wg.Wait()
		if missing.Load() {
			return reads.Load(), errors.New("readlink current returned ENOENT during a flip")
		}
		return reads.Load(), nil
	}
}

// finishWatch stops the reader and fails the test if a read found no link,
// or if the reader made fewer reads than there were flips, since a reader
// that ran late would otherwise pass without having watched anything.
func finishWatch(t *testing.T, stop chan struct{}, wait func() (int64, error), flips int) {
	t.Helper()
	close(stop)
	reads, err := wait()
	if err != nil {
		t.Fatal(err)
	}
	if reads < int64(flips) {
		t.Fatalf("reader made %d reads over %d flips, want at least one per flip", reads, flips)
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
	started, wait := watchCurrent(link, stop)
	<-started
	const flips = 1500
	versions := []string{"2.0.0", "1.0.0"}
	for i := 0; i < flips; i++ {
		if err := Activate("testpack", versions[i%2]); err != nil {
			close(stop)
			_, _ = wait()
			t.Fatalf("Activate: %v", err)
		}
	}
	finishWatch(t, stop, wait, flips)

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
	started, wait := watchCurrent(link, stop)
	<-started
	for i := 2; i < 60; i++ {
		if err := InstallFromLocal("testpack", mk(fmt.Sprintf("%d.0.0", i)), true); err != nil {
			close(stop)
			_, _ = wait()
			t.Fatalf("install %d: %v", i, err)
		}
	}
	finishWatch(t, stop, wait, 58)
	if got, err := os.Readlink(link); err != nil || got != "59.0.0" {
		t.Errorf("current = %q (err %v), want 59.0.0", got, err)
	}
	for _, n := range entryNames(t, filepath.Join(PacksDir(), "testpack")) {
		if len(n) > 0 && n[0] == '.' {
			t.Errorf("temp entry %q left in pack dir", n)
		}
	}
}

// A temp link left by a crashed process whose pid this process now holds
// does not make the next flip fail: the sequence restarts at 1 in every
// process, so the leftover can carry exactly the name the flip picks.
func TestFlipCurrent_ReplacesALeftoverTempLinkOfTheSameName(t *testing.T) {
	freezeSafeHome(t)
	for _, v := range []string{"1.0.0", "2.0.0"} {
		writePackYAML(t, filepath.Join(PacksDir(), "testpack", v), v)
	}
	if err := Activate("testpack", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(PacksDir(), "testpack")
	leftover := filepath.Join(dir, fmt.Sprintf(".current.%d.%d", os.Getpid(), flipSeq.Load()+1))
	if err := os.Symlink("1.0.0", leftover); err != nil {
		t.Fatal(err)
	}

	if err := Activate("testpack", "2.0.0"); err != nil {
		t.Fatalf("Activate with a leftover temp link of the same name: %v", err)
	}
	if got, err := os.Readlink(filepath.Join(dir, "current")); err != nil || got != "2.0.0" {
		t.Errorf("current = %q (err %v), want 2.0.0", got, err)
	}
}
