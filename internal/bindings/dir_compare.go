package bindings

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// sameDirContent reports whether two directories carry the same file
// set with byte-identical content (relative paths compared; modes and
// times ignored; symlinks compared by target). Used to distinguish a
// benign duplicate custom skill (a second checkout of the same
// project) from a genuine content collision.
func sameDirContent(a, b string) (bool, error) {
	am, err := dirDigest(a)
	if err != nil {
		return false, err
	}
	bm, err := dirDigest(b)
	if err != nil {
		return false, err
	}
	if len(am) != len(bm) {
		return false, nil
	}
	for rel, sum := range am {
		if !bytes.Equal(bm[rel], sum) {
			return false, nil
		}
	}
	return true, nil
}

// dirDigest maps each relative path under root to a content digest.
func dirDigest(root string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, linkErr := os.Readlink(path)
			if linkErr != nil {
				return linkErr
			}
			sum := sha256.Sum256([]byte("symlink\x00" + target))
			out[rel] = sum[:]
			return nil
		}
		f, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, f)
		_ = f.Close()
		if copyErr != nil {
			return copyErr
		}
		out[rel] = h.Sum(nil)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("digest %s: %w", root, err)
	}
	return out, nil
}
