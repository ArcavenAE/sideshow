package factoryguard

import (
	"os"
	"testing"
	"time"
)

// Env-gated real-tree observation (same pattern as the bindings
// package's VSDD_TREE tests): run the guard read-only against an
// actual factory checkout and print what it sees.
func TestCheckRepo_RealTree(t *testing.T) {
	repo := os.Getenv("VSDD_FACTORY_REPO")
	if repo == "" {
		t.Skip("VSDD_FACTORY_REPO not set; real-tree guard observation skipped")
	}
	v := CheckRepo(repo, time.Now())
	t.Logf("in-flight=%v hard=%v\n%s", v.InFlight(), v.HardRefusal(), v.Refusal())
}
