package bindings

import (
	"fmt"
	"os"
)

// writeWithSourceMode writes data to target carrying the source file's
// permission bits, so executables in a pack stay executable at the
// destination (the store-side half of this fix is verifyExecManifest at
// install time). os.WriteFile applies its mode only on create, so an
// existing target from an earlier sync is chmodded into agreement.
func writeWithSourceMode(target string, data []byte, srcPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("stat source %s: %w", srcPath, err)
	}
	mode := info.Mode().Perm()
	if err := os.WriteFile(target, data, mode); err != nil {
		return err
	}
	return os.Chmod(target, mode)
}
