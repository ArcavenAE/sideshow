package pack

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

var flipSeq atomic.Uint64

// flipCurrent points the link at version in one step. It builds a
// temporary link in the same directory and renames it over the link, so a
// reader sees the old target or the new one and never a missing link.
// A failed rename leaves the previous link in place.
func flipCurrent(link, version string) error {
	tmp := filepath.Join(filepath.Dir(link), fmt.Sprintf(".current.%d.%d", os.Getpid(), flipSeq.Add(1)))
	if err := os.Symlink(version, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
