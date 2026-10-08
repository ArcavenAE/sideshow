package bindings

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic replaces path with data by writing a temp file in the
// same directory, syncing it, and renaming it over the old file, so a
// reader or a later run sees the old file or the whole new one, never a
// torn write (sideshow#173). The temp file is removed on any failure.
//
// It behaves like os.WriteFile(path, data, perm) in what a user can see:
// an existing file keeps its mode, a new file gets perm minus the umask
// (the temp file is created with perm, so the kernel applies the umask),
// and a symlink at path is followed, so the link stays and its target is
// replaced.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, rerr := filepath.EvalSymlinks(path)
		if rerr != nil {
			return fmt.Errorf("resolve symlink %s: %w", path, rerr)
		}
		path = resolved
	}
	mode := perm
	existing := false
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		existing = true
	}

	tmp, tmpName, err := createTempBeside(path, perm)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if existing {
		if err := tmp.Chmod(mode); err != nil {
			_ = tmp.Close()
			cleanup()
			return fmt.Errorf("chmod temp file: %w", err)
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

// createTempBeside creates a new file next to path with the given mode
// (before the umask), failing if the name is taken.
func createTempBeside(path string, perm os.FileMode) (*os.File, string, error) {
	dir, base := filepath.Split(path)
	for range 10 {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, "", err
		}
		name := filepath.Join(dir, ".tmp-"+base+"-"+hex.EncodeToString(b[:]))
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
		if err == nil {
			return f, name, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("no unused temp name beside %s", path)
}
