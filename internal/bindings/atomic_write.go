package bindings

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	path, err := followLinks(path)
	if err != nil {
		return err
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

// followLinks returns the path a write to path lands on. EvalSymlinks
// answers when the target exists. When it does not (a dangling link, or a
// new file), the chain is walked by hand, and each hop resolves its own
// directory with EvalSymlinks before the link's target is applied, never
// lexically: a `..` in a relative target climbs the physical directory, as
// the kernel does, even when the link's directory is itself a symlink. A
// missing directory is an error, as for an in-place write.
func followLinks(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	for range 40 {
		dir, base := ".", path
		if i := strings.LastIndexByte(path, os.PathSeparator); i >= 0 {
			dir, base = path[:i], path[i+1:]
			if dir == "" {
				dir = string(os.PathSeparator)
			}
		}
		physDir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return "", fmt.Errorf("resolve directory of %s: %w", path, err)
		}
		path = filepath.Join(physDir, base)
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return "", fmt.Errorf("read symlink %s: %w", path, err)
		}
		if filepath.IsAbs(target) {
			path = target
		} else {
			path = physDir + string(os.PathSeparator) + target
		}
	}
	return "", fmt.Errorf("too many levels of symbolic links at %s", path)
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
