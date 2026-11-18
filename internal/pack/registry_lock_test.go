//go:build unix

package pack

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func registryNames(t *testing.T) map[string]bool {
	t.Helper()
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range reg.Packs {
		got[p.Name] = true
	}
	return got
}

// Activating many packs at once records every one of them: the registry
// read, modify and write of one activation is not interleaved with
// another's, so no update is lost.
func TestActivate_ConcurrentActivationsAreAllRecorded(t *testing.T) {
	freezeSafeHome(t)
	const n = 24
	for i := 0; i < n; i++ {
		writePackYAML(t, filepath.Join(PacksDir(), fmt.Sprintf("pack%02d", i), "1.0.0"), "1.0.0")
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			errs <- Activate(name, "1.0.0")
		}(fmt.Sprintf("pack%02d", i))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Activate: %v", err)
		}
	}
	got := registryNames(t)
	if len(got) != n {
		t.Errorf("registry holds %d of %d packs after concurrent activations: %v", len(got), n, got)
	}
}

// Two names installed at once both appear in registry.yaml.
func TestInstall_ConcurrentInstallsOfTwoNamesAreBothRecorded(t *testing.T) {
	freezeSafeHome(t)
	mk := func() string {
		src := t.TempDir()
		cfg := filepath.Join(src, "_config")
		if err := os.MkdirAll(cfg, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg, "manifest.yaml"), []byte("installation:\n  version: \"1.0.0\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return src
	}
	const n = 8
	srcs := make([]string, n)
	for i := range srcs {
		srcs[i] = mk()
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- InstallFromLocal(fmt.Sprintf("pack%02d", i), srcs[i], true)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Install: %v", err)
		}
	}
	got := registryNames(t)
	if len(got) != n {
		t.Errorf("registry holds %d of %d packs after concurrent installs: %v", len(got), n, got)
	}
}
