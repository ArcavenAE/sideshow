//go:build unix

package pack

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// registryLockPath is the store-wide lock file. It sits beside
// registry.yaml, outside packs/, so no version walk sees it.
func registryLockPath() string {
	return filepath.Join(SideshowDir(), ".registry.lock")
}

// UpdateRegistry runs one read, modify and write of registry.yaml under an
// exclusive store-wide lock: it loads the registry, calls fn, and saves it
// when fn returns nil. Two updates never interleave, so neither loses the
// other's change. The lock is advisory and is released when this call
// returns or the process exits. fn must not call UpdateRegistry.
func UpdateRegistry(fn func(*Registry) error) error {
	if err := os.MkdirAll(SideshowDir(), 0o755); err != nil {
		return fmt.Errorf("create sideshow dir: %w", err)
	}
	f, err := os.OpenFile(registryLockPath(), os.O_CREATE|os.O_RDONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open registry lock: %w", err)
	}
	defer func() { _ = f.Close() }()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("lock registry: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	reg, err := LoadRegistry()
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	if err := fn(reg); err != nil {
		return err
	}
	return reg.Save()
}
