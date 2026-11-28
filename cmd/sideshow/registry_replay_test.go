//go:build unix

package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// A project another command adds to the registry while project init is
// running survives init's registry write: the write replays init's own
// results onto the registry as it is under the lock, not onto the copy init
// read at its start. The overlap is by timing, so this test can only fail
// when the other write lands between init's first read and its write.
func TestProjectInit_ConcurrentRegistryAdditionsSurvive(t *testing.T) {
	linksFixture(t)
	for i := 0; i < 15; i++ {
		other := fmt.Sprintf("other-%02d", i)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pack.UpdateRegistry(func(r *pack.Registry) error {
				r.FindOrCreateProject(other)
				return nil
			})
		}()
		if _, err := captureStdout(t, func() error {
			return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
		}); err != nil {
			t.Fatalf("project init: %v", err)
		}
		wg.Wait()
		reg, err := pack.LoadRegistry()
		if err != nil {
			t.Fatal(err)
		}
		if reg.FindProject(other) == nil {
			t.Fatalf("project %s added during init is missing from the registry", other)
		}
	}
}
