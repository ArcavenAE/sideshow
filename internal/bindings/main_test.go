package bindings

import (
	"os"
	"testing"
)

// TestMain clears CLAUDE_CONFIG_DIR so tests that isolate by setting
// HOME write under their temp HOME, never into the config dir of the
// harness running the tests. A test that needs the variable sets it
// with t.Setenv.
func TestMain(m *testing.M) {
	_ = os.Unsetenv("CLAUDE_CONFIG_DIR")
	os.Exit(m.Run())
}
