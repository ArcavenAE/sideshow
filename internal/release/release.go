// Package release verifies a signed sideshow-packs release before any of
// it reaches the store (aae-orc-wk92, with aae-orc-d3nq.19 parts a and b).
//
// The recipe is sideshow-packs docs/release-url-format.md: verify the
// signature bundle, verify the attestation, read the provenance from the
// verified attestation payload, check the tarball and manifests against the
// attested hashes, then check the extracted tree against the manifests.
package release

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/atomicfile"
)

// PredicateType is the attestation predicate type current releases carry.
const PredicateType = "https://arcaven.com/sideshow/install-meta"

// LegacyPredicateType is the versioned type releases cut before 2026-08-01 carry.
const LegacyPredicateType = "https://arcaven.com/sideshow/install-meta/v0.1.0"

const (
	// AttestIdentityRegexp pins the signer of the attestation: the build or
	// the re-attest workflow of sideshow-packs, run from main.
	AttestIdentityRegexp = `^https://github\.com/ArcavenAE/sideshow-packs/\.github/workflows/(build-pack|reattest-release)\.yml@refs/heads/main$`
	// BlobIdentityRegexp pins the signer of the tarball: the build workflow,
	// from main or from a pack-version tag such as bmad-v6.12.4-r3.
	BlobIdentityRegexp = `^https://github\.com/ArcavenAE/sideshow-packs/\.github/workflows/build-pack\.yml@refs/(heads/main|tags/[a-z0-9][a-z0-9-]*-v[0-9][^/]*)$`
	certOIDCIssuer     = "https://token.actions.githubusercontent.com"

	// maxExtractBytes caps the bytes written while extracting one tarball.
	maxExtractBytes = 1 << 30
)

// Verifier checks Sigstore bundles. The real one runs the cosign binary;
// tests supply a fake.
type Verifier interface {
	VerifyBlob(bundle, artifact string) error
	VerifyAttestation(bundle, artifact, predicateType string) error
}

// Options tunes Verify.
type Options struct {
	// SHA256 pins the tarball digest; empty means no pin.
	SHA256 string
}

// Result is a verified, extracted release.
type Result struct {
	// Root is the extracted pack root, ready for pack.Install.
	Root string
	// TarballSHA256 is the digest of the verified tarball.
	TarballSHA256 string
	// SchemaVersion is read from the verified predicate.
	SchemaVersion string
	// Warnings lists checks that were unavailable rather than failed.
	Warnings []string
	// Cleanup removes the extraction directory.
	Cleanup func()
}

const tarballSuffix = "-arcaven.tar.gz"

// Locate reports the release folder and tarball that from names: a
// .tar.gz file, or a folder holding exactly one <pack>-<ver>-arcaven.tar.gz.
// ok is false for anything else (a plain pack directory).
func Locate(from string) (dir, tarball string, ok bool) {
	info, err := os.Stat(from)
	if err != nil {
		return "", "", false
	}
	if !info.IsDir() {
		if strings.HasSuffix(from, ".tar.gz") {
			return filepath.Dir(from), from, true
		}
		return "", "", false
	}
	matches, err := filepath.Glob(filepath.Join(from, "*"+tarballSuffix))
	if err != nil || len(matches) != 1 {
		return "", "", false
	}
	return from, matches[0], true
}

// Verify checks the release named by from and extracts it to a temp dir.
// A refused release leaves nothing behind and returns no Result.
func Verify(from, pack string, v Verifier, opts Options) (*Result, error) {
	dir, tarball, ok := Locate(from)
	if !ok {
		return nil, fmt.Errorf("%s is not a signed release (no %s tarball)", from, "<pack>-<ver>"+tarballSuffix)
	}
	sigBundle := tarball + ".bundle"
	// The v2 bundle carries a dsseEnvelope; releases published before it
	// have only the older .attest.bundle.
	attBundle := tarball + ".attest.v2.bundle"
	if _, err := os.Stat(attBundle); errors.Is(err, fs.ErrNotExist) {
		attBundle = tarball + ".attest.bundle"
	}
	for _, b := range []string{sigBundle, attBundle} {
		if _, err := os.Stat(b); err != nil {
			return nil, fmt.Errorf("%s is missing from the release: %w", filepath.Base(b), err)
		}
	}

	if err := v.VerifyBlob(sigBundle, tarball); err != nil {
		return nil, fmt.Errorf("signature check for %s: %w", filepath.Base(tarball), err)
	}
	if err := v.VerifyAttestation(attBundle, tarball, PredicateType); err != nil {
		if err2 := v.VerifyAttestation(attBundle, tarball, LegacyPredicateType); err2 != nil {
			return nil, fmt.Errorf("attestation check for %s: %w", filepath.Base(tarball), err)
		}
	}

	pred, err := readPredicate(attBundle)
	if err != nil {
		return nil, err
	}
	if !schemaAccepted(pred.SchemaVersion) {
		return nil, fmt.Errorf("attested schema_version %q is not one this sideshow reads (0.1.x, 0.2.x)", pred.SchemaVersion)
	}

	digest, err := fileSHA256(tarball)
	if err != nil {
		return nil, err
	}
	if pred.Artifact.TarballSHA256 != digest {
		return nil, fmt.Errorf("tarball_sha256 mismatch: attested %q, tarball is %s", pred.Artifact.TarballSHA256, digest)
	}
	if opts.SHA256 != "" && !strings.EqualFold(opts.SHA256, digest) {
		return nil, fmt.Errorf("sha256 pin mismatch: pinned %s, tarball is %s", opts.SHA256, digest)
	}

	var warnings []string
	fileManifest, w, err := attestedManifest(dir, "file-manifest.csv", "file_manifest_sha256", pred.Artifact.FileManifestSHA256)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, w...)
	execManifest, w, err := attestedManifest(dir, "exec-manifest.txt", "exec_manifest_sha256", pred.Artifact.ExecManifestSHA256)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, w...)

	extractDir, err := os.MkdirTemp("", "sideshow-release-")
	if err != nil {
		return nil, fmt.Errorf("make extraction dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(extractDir) }
	root, err := extract(tarball, extractDir)
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := checkTree(root, fileManifest, execManifest); err != nil {
		cleanup()
		return nil, err
	}
	return &Result{
		Root:          root,
		TarballSHA256: digest,
		SchemaVersion: pred.SchemaVersion,
		Warnings:      warnings,
		Cleanup:       cleanup,
	}, nil
}

type predicate struct {
	SchemaVersion string `json:"schema_version"`
	Artifact      struct {
		TarballSHA256      string `json:"tarball_sha256"`
		FileManifestSHA256 string `json:"file_manifest_sha256"`
		ExecManifestSHA256 string `json:"exec_manifest_sha256"`
	} `json:"artifact"`
}

// readPredicate reads the provenance from the attestation payload. Only
// call it after the attestation verified: the payload is what was signed.
func readPredicate(attBundle string) (*predicate, error) {
	raw, err := os.ReadFile(attBundle)
	if err != nil {
		return nil, err
	}
	var env struct {
		DSSE *struct {
			Payload string `json:"payload"`
		} `json:"dsseEnvelope"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(attBundle), err)
	}
	if env.DSSE == nil || env.DSSE.Payload == "" {
		return nil, fmt.Errorf("%s has no dsseEnvelope payload to read the provenance from", filepath.Base(attBundle))
	}
	payload, err := base64.StdEncoding.DecodeString(env.DSSE.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode attestation payload: %w", err)
	}
	var stmt struct {
		Predicate predicate `json:"predicate"`
	}
	if err := json.Unmarshal(payload, &stmt); err != nil {
		return nil, fmt.Errorf("parse attestation payload: %w", err)
	}
	return &stmt.Predicate, nil
}

// schemaAccepted reports whether v is a schema_version this reader knows:
// 0.1.x or 0.2.x, with an optional prerelease suffix.
func schemaAccepted(v string) bool {
	base := v
	if i := strings.IndexByte(base, '-'); i >= 0 {
		base = base[:i]
	}
	return strings.HasPrefix(base, "0.1.") || strings.HasPrefix(base, "0.2.")
}

// attestedManifest returns the bytes of a manifest in the release folder,
// checked against the attested hash. With no attested hash it warns and
// returns the folder copy unchecked (nil if absent).
func attestedManifest(dir, name, field, want string) ([]byte, []string, error) {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if want == "" {
		w := fmt.Sprintf("attestation carries no %s: the manifest hash check for %s is unavailable", field, name)
		if err != nil {
			return nil, []string{w}, nil
		}
		return b, []string{w}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%s is attested (%s) but not in the release folder: %w", name, field, err)
	}
	if got := sumHex(b); got != want {
		return nil, nil, fmt.Errorf("%s mismatch: attested %q, %s is %s", field, want, name, got)
	}
	return b, nil, nil
}

func sumHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extract unpacks tarball into dest and returns the pack root: the single
// top-level directory when there is one, else dest. Only regular files and
// directories are written; a path that leaves dest is refused.
func extract(tarball, dest string) (string, error) {
	f, err := os.Open(tarball)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", filepath.Base(tarball), err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	prefix := filepath.Clean(dest) + string(os.PathSeparator)
	var written int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read %s: %w", filepath.Base(tarball), err)
		}
		target := filepath.Join(dest, h.Name)
		if !strings.HasPrefix(target, prefix) {
			return "", fmt.Errorf("tar entry %q escapes the extraction dir", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			// Each file is read whole, bounded by maxExtractBytes for the tarball.
			data, err := io.ReadAll(io.LimitReader(tr, maxExtractBytes-written+1))
			if err != nil {
				return "", fmt.Errorf("read %q from %s: %w", h.Name, filepath.Base(tarball), err)
			}
			written += int64(len(data))
			if written > maxExtractBytes {
				return "", fmt.Errorf("%s expands past %d bytes", filepath.Base(tarball), int64(maxExtractBytes))
			}
			if err := atomicfile.WriteFile(target, data, os.FileMode(h.Mode&0o777)); err != nil {
				return "", err
			}
			// The write applies the umask; set the exact mode so the exec bit holds.
			if err := os.Chmod(target, os.FileMode(h.Mode&0o777)); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("tar entry %q has type %q; only files and directories are installed", h.Name, string(h.Typeflag))
		}
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(dest, entries[0].Name()), nil
	}
	return dest, nil
}

// checkTree verifies the extracted tree against the manifests: the sha256
// of each file in file-manifest.csv (its size column is not read, since the
// digest covers it), and the exec bit on each path in exec-manifest.txt. A
// nil manifest is skipped (its hash was not attested).
func checkTree(root string, fileManifest, execManifest []byte) error {
	for _, line := range strings.Split(string(fileManifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ",", 3)
		if len(parts) != 3 {
			return fmt.Errorf("file-manifest.csv line %q is not sha256,size,path", line)
		}
		path, err := safeJoin(root, parts[2])
		if err != nil {
			return err
		}
		got, err := fileSHA256(path)
		if err != nil {
			return fmt.Errorf("file %s is in file-manifest.csv but not in the tarball: %w", parts[2], err)
		}
		if !strings.EqualFold(got, parts[0]) {
			return fmt.Errorf("file %s does not match file-manifest.csv: sha256 %s, manifest %s", parts[2], got, parts[0])
		}
	}
	for _, line := range strings.Split(string(execManifest), "\n") {
		rel := strings.TrimSpace(line)
		if rel == "" {
			continue
		}
		path, err := safeJoin(root, rel)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("file %s is in exec-manifest.txt but not in the tarball: %w", rel, err)
		}
		if info.Mode()&0o111 == 0 {
			return fmt.Errorf("file %s must be executable (exec-manifest.txt) but its mode is %v", rel, info.Mode().Perm())
		}
	}
	return nil
}

// safeJoin joins a manifest path under root and refuses one that leaves it.
func safeJoin(root, rel string) (string, error) {
	p := filepath.Join(root, rel)
	if !strings.HasPrefix(p, filepath.Clean(root)+string(os.PathSeparator)) {
		return "", fmt.Errorf("manifest path %q escapes the pack root", rel)
	}
	return p, nil
}

type cosign struct{ path string }

// NewCosign returns a Verifier that runs the cosign binary at path
// (looked up on PATH when empty), pinned to the sideshow-packs build
// workflow identity.
func NewCosign(path string) (Verifier, error) {
	if path == "" {
		path = "cosign"
	}
	p, err := exec.LookPath(path)
	if err != nil {
		return nil, fmt.Errorf("cosign is required to verify a signed release and was not found: %w", err)
	}
	return &cosign{path: p}, nil
}

func (c *cosign) run(identity string, args ...string) error {
	args = append(args,
		"--certificate-identity-regexp", identity,
		"--certificate-oidc-issuer", certOIDCIssuer,
	)
	out, err := exec.Command(c.path, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("cosign %s: %s", args[0], msg)
	}
	return nil
}

func (c *cosign) VerifyBlob(bundle, artifact string) error {
	return c.run(BlobIdentityRegexp, "verify-blob", "--bundle", bundle, artifact)
}

func (c *cosign) VerifyAttestation(bundle, artifact, predicateType string) error {
	return c.run(AttestIdentityRegexp, "verify-blob-attestation", "--bundle", bundle, "--type", predicateType, artifact)
}
