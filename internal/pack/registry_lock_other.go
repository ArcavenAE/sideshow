//go:build !unix

package pack

import "errors"

// UpdateRegistry has no store-wide lock on this platform, so it refuses
// rather than write the registry unprotected.
func UpdateRegistry(fn func(*Registry) error) error {
	return errors.New("registry lock is not supported on this platform")
}
