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
//
// The owner write bit is always kept: served bindings are
// sideshow-owned regenerable output, not store content. Carrying a
// frozen source's 0444 verbatim left the next sync unable to
// overwrite its own output, which made every other version flip
// remove everything and sync nothing (sideshow#108).
func writeWithSourceMode(target string, data []byte, srcPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("stat source %s: %w", srcPath, err)
	}
	mode := info.Mode().Perm() | 0o200
	// A target written before this fix may sit read-only on disk;
	// unlock it in place so the write below can replace it
	// (self-heals machines that synced from a frozen store).
	if fi, statErr := os.Stat(target); statErr == nil && fi.Mode().Perm()&0o200 == 0 {
		if chmodErr := os.Chmod(target, fi.Mode().Perm()|0o200); chmodErr != nil {
			return fmt.Errorf("unlock existing binding %s: %w", target, chmodErr)
		}
	}
	if err := os.WriteFile(target, data, mode); err != nil {
		return err
	}
	return os.Chmod(target, mode)
}
