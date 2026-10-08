package enable

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/atomicfile"
	"github.com/ArcavenAE/sideshow/internal/bindings"
)

// Byte-exact removal of the settings file (aae-orc-gf80m).
//
// Every settings writer in the bindings package parses the file and
// writes it back in canonical form, so after enable the bytes are no
// longer the user's. Enable keeps the original bytes in a sidecar and
// the sha of the file as enable left it. Disable restores the original
// bytes only when the file still hashes to that sha, meaning nobody
// touched it since; otherwise it removes exactly what enable added and
// says the file was rewritten in canonical form.
//
// Of the six JSON writers in the tree, only the bindings package's
// writeSettings runs inside Enable (through MergeEnvShim and
// MergeHookChain, and their Remove* twins on rollback and in Disable).
// The others belong to other verbs: adopt's agent key, distribute's
// hook merge, activate's persona flip, foreign suppression, and the
// permissions installer.
//
// The sidecar can hold env values, so it is written 0600 in a 0700
// directory beside the ledger in the sideshow store, never in the repo,
// and removed when its row is.

const (
	sidecarDirName = "settings-originals"
	sidecarMagic   = "sideshow-settings-original v1\n"
)

func sidecarDir(ledgerPath string) string {
	return filepath.Join(filepath.Dir(ledgerPath), sidecarDirName)
}

// sidecarName is stable per repo, pack, and scope, so the ledger row
// needs no field for it and an older sideshow never meets a new tag.
func sidecarName(repoDir, pack, scope string) string {
	sum := sha256.Sum256([]byte(repoDir + "\x00" + pack + "\x00" + scope))
	return hex.EncodeToString(sum[:16]) + ".orig"
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// writeSidecar stores the original bytes and the sha of the settings
// file as enable left it. Layout: magic line, "enabled-sha256: <hex>",
// then the original bytes verbatim.
func writeSidecar(ledgerPath, name, enabledSHA string, original []byte) error {
	dir := sidecarDir(ledgerPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create settings sidecar dir: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString(sidecarMagic)
	buf.WriteString("enabled-sha256: " + enabledSHA + "\n")
	buf.Write(original)

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create settings sidecar: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod settings sidecar: %w", err)
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write settings sidecar: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write settings sidecar: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("place settings sidecar: %w", err)
	}
	return nil
}

// readSidecar returns the enabled sha and the original bytes.
func readSidecar(ledgerPath, name string) (enabledSHA string, original []byte, err error) {
	data, err := os.ReadFile(filepath.Join(sidecarDir(ledgerPath), name))
	if err != nil {
		return "", nil, err
	}
	rest, ok := bytes.CutPrefix(data, []byte(sidecarMagic))
	if !ok {
		return "", nil, fmt.Errorf("unrecognized sidecar header")
	}
	line, orig, ok := bytes.Cut(rest, []byte("\n"))
	sha, hasPrefix := strings.CutPrefix(string(line), "enabled-sha256: ")
	if !ok || !hasPrefix || len(sha) != 64 {
		return "", nil, fmt.Errorf("malformed sidecar sha line")
	}
	return sha, orig, nil
}

func removeSidecar(ledgerPath, name string) {
	if name == "" {
		return
	}
	if err := os.Remove(filepath.Join(sidecarDir(ledgerPath), name)); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "warning: remove settings sidecar %s: %v\n", name, err)
	}
}

// sameJSON reports whether two settings documents parse to equal values.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// restoreOriginalSettings runs after Disable's semantic removal. When
// the file was unchanged since enable, it puts the original bytes
// back. before is the file's sha as Disable found it. It prints one
// line whenever it falls back, and deletes the sidecar either way.
func restoreOriginalSettings(ledgerPath, settings, ref, beforeSHA string, createdByEnable bool) {
	defer removeSidecar(ledgerPath, ref)
	if createdByEnable {
		return
	}
	if beforeSHA == "" {
		// The settings file is gone (the repo was deleted or reset
		// after enable): nothing to restore and nothing was rewritten.
		return
	}
	canonical := func(why string) {
		fmt.Printf("note: %s %s; disable removed exactly what enable added and rewrote the file in canonical form\n", settings, why)
	}
	enabledSHA, original, err := readSidecar(ledgerPath, ref)
	if os.IsNotExist(err) {
		canonical("has no record of its original bytes (enabled by an older sideshow)")
		return
	}
	if err != nil {
		canonical(fmt.Sprintf("has an unreadable record of its original bytes (%v)", err))
		return
	}
	now, err := os.ReadFile(settings)
	if beforeSHA != enabledSHA && (err != nil || !isOwnRendering(now, original)) {
		// The file differs from the enabled bytes. A pass killed after
		// the rewrite leaves exactly sideshow's rendering of the
		// original; anything else is a hand edit and keeps its bytes.
		canonical("changed since enable")
		return
	}
	if err != nil || !sameJSON(now, original) {
		canonical("did not match its original content after removal")
		return
	}
	if err := atomicfile.WriteFile(settings, original, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: restore %s: %v\n", settings, err)
		canonical("could not be restored")
	}
}

// isOwnRendering reports whether now is byte-for-byte what sideshow's
// settings writer renders from the original bytes' content. A hand edit
// that only reformats the file does not match.
func isOwnRendering(now, original []byte) bool {
	var m map[string]any
	if err := json.Unmarshal(original, &m); err != nil {
		return false
	}
	want, err := bindings.RenderSettings(m)
	return err == nil && bytes.Equal(now, want)
}
