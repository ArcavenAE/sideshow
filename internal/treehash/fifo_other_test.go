//go:build !unix

package treehash

import "errors"

func mkfifo(string) error { return errors.New("unsupported") }
