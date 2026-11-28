package pack

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
)

var flipSeq atomic.Uint64

// flipCurrent points the link at version in one step. It builds a
// temporary link in the same directory and renames it over the link, so a
// reader sees the old target or the new one and never a missing link.
// A failed rename leaves the previous link in place. A temp link of the
// same name left by a dead process that held this pid is replaced once.
func flipCurrent(link, version string) error {
	tmp := filepath.Join(filepath.Dir(link), fmt.Sprintf(".current.%d.%d", os.Getpid(), flipSeq.Add(1)))
	err := os.Symlink(version, tmp)
	if errors.Is(err, fs.ErrExist) {
		_ = os.Remove(tmp)
		err = os.Symlink(version, tmp)
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
