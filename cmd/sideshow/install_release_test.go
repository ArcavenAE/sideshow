package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

type okVerifier struct{}

func (okVerifier) VerifyBlob(bundle, artifact string) error                       { return nil }
func (okVerifier) VerifyAttestation(bundle, artifact, predicateType string) error { return nil }

// writeRelease builds a minimal signed-release folder for pack "demo" 1.0.0
// (shape A: the attested predicate carries only the tarball digest). With
// tamper set, the tarball is changed after the digest was taken.
func writeRelease(t *testing.T, tamper bool) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
	tarPath := filepath.Join(dir, "demo-1.0.0-arcaven.tar.gz")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte("name: demo\nversion: 1.0.0\n")
	if err := tw.WriteHeader(&tar.Header{Name: "demo-1.0.0/pack.yaml", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(tarPath)
	h := sha256.Sum256(b)
	sha = hex.EncodeToString(h[:])
	if tamper {
		if err := os.WriteFile(tarPath, append(b, 0), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stmt, _ := json.Marshal(map[string]any{"predicate": map[string]any{
		"schema_version": "0.1.1",
		"artifact":       map[string]any{"tarball_sha256": sha},
	}})
	bundle, _ := json.Marshal(map[string]any{"dsseEnvelope": map[string]any{"payload": base64.StdEncoding.EncodeToString(stmt)}})
	for name, data := range map[string][]byte{
		"demo-1.0.0-arcaven.tar.gz.bundle":        []byte("{}"),
		"demo-1.0.0-arcaven.tar.gz.attest.bundle": bundle,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, sha
}

func setupHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	t.Cleanup(func() { _ = pack.UnfreezeTree(home) })
	releaseVerifier = okVerifier{}
	t.Cleanup(func() { releaseVerifier = nil })
}

func TestRunInstall_SignedReleaseInstallsAndRecordsTheDigest(t *testing.T) {
	setupHome(t)
	dir, sha := writeRelease(t, false)
	if err := runInstall([]string{"demo", "--from", dir, "--yes"}); err != nil {
		t.Fatalf("install from a release folder: %v", err)
	}
	reg, err := pack.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var got *pack.InstalledPack
	for i := range reg.Packs {
		if reg.Packs[i].Name == "demo" {
			got = &reg.Packs[i]
		}
	}
	if got == nil {
		t.Fatal("demo is not in the registry")
	}
	if got.TarballSHA256 != sha || !got.Verified {
		t.Errorf("registry entry = {sha %q, verified %v}, want {%q, true}", got.TarballSHA256, got.Verified, sha)
	}
}

func TestRunInstall_TamperedReleaseLeavesTheStoreEmpty(t *testing.T) {
	setupHome(t)
	dir, _ := writeRelease(t, true)
	err := runInstall([]string{"demo", "--from", dir, "--yes"})
	if err == nil || !strings.Contains(err.Error(), "tarball_sha256") {
		t.Fatalf("install of a tampered release: err = %v, want one naming tarball_sha256", err)
	}
	if _, serr := os.Stat(filepath.Join(pack.PacksDir(), "demo")); !os.IsNotExist(serr) {
		t.Errorf("a refused release left %s in the store (stat err %v)", filepath.Join(pack.PacksDir(), "demo"), serr)
	}
}

func TestRunInstall_ShaPinFlagRefusesADifferentTarball(t *testing.T) {
	setupHome(t)
	dir, _ := writeRelease(t, false)
	err := runInstall([]string{"demo", "--from", dir, "--yes", "--sha256", strings.Repeat("0", 64)})
	if err == nil || !strings.Contains(err.Error(), "pin") {
		t.Fatalf("install with a wrong --sha256: err = %v, want one naming the pin", err)
	}
}

func TestRunInstall_PlainDirectoryStillInstallsUnverified(t *testing.T) {
	setupHome(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte("name: demo\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runInstall([]string{"demo", "--from", src, "--yes"}); err != nil {
		t.Fatalf("plain directory install: %v", err)
	}
	reg, _ := pack.LoadRegistry()
	for _, p := range reg.Packs {
		if p.Name == "demo" && (p.Verified || p.TarballSHA256 != "") {
			t.Errorf("a plain-directory install was recorded as verified: %+v", p)
		}
	}
}
