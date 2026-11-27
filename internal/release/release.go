// Package release verifies a signed sideshow-packs release before any of
// it reaches the store (aae-orc-wk92, with aae-orc-d3nq.19 parts a and b).
//
// This file holds the API only; the checks are not written yet.
package release

import "errors"

// PredicateType is the attestation predicate type current releases carry.
const PredicateType = "https://arcaven.com/sideshow/install-meta"

// LegacyPredicateType is the versioned type releases cut before 2026-08-01 carry.
const LegacyPredicateType = "https://arcaven.com/sideshow/install-meta/v0.1.0"

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

var errNotImplemented = errors.New("release verification not implemented")

// Locate reports the release folder and tarball that from names: a
// .tar.gz file, or a folder holding exactly one <pack>-<ver>-arcaven.tar.gz.
// ok is false for anything else (a plain pack directory).
func Locate(from string) (dir, tarball string, ok bool) { return "", "", false }

// Verify checks the release named by from and extracts it to a temp dir.
func Verify(from, pack string, v Verifier, opts Options) (*Result, error) {
	return nil, errNotImplemented
}

// NewCosign returns a Verifier that runs the cosign binary at path
// (looked up on PATH when empty), pinned to the sideshow-packs build
// workflow identity.
func NewCosign(path string) (Verifier, error) { return nil, errNotImplemented }
