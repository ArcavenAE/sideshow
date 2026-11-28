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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fakeVerifier stands in for cosign. blobErr and attErr are returned for the
// matching call; attFailTypes makes VerifyAttestation fail for those types only.
type fakeVerifier struct {
	blobErr      error
	attErr       error
	attFailTypes map[string]bool
	calls        []string
	attTypes     []string
	blobArgs     [][2]string // (bundle, artifact) per VerifyBlob call
	attArgs      [][3]string // (bundle, artifact, predicateType) per VerifyAttestation call
}

func (f *fakeVerifier) VerifyBlob(bundle, artifact string) error {
	f.calls = append(f.calls, "blob")
	f.blobArgs = append(f.blobArgs, [2]string{bundle, artifact})
	return f.blobErr
}

func (f *fakeVerifier) VerifyAttestation(bundle, artifact, predicateType string) error {
	f.calls = append(f.calls, "attest")
	f.attTypes = append(f.attTypes, predicateType)
	f.attArgs = append(f.attArgs, [3]string{bundle, artifact, predicateType})
	if f.attFailTypes[predicateType] {
		return fmt.Errorf("predicate type mismatch: %s", predicateType)
	}
	return f.attErr
}

func hexSHA(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type fixture struct {
	dir     string
	tarball string
	tarSHA  string
}

type buildOpts struct {
	schema          string // predicate schema_version; default 0.2.0
	omitManifestSHA bool   // shape A: no manifest hashes in the predicate
	loseExecBit     bool   // tarball entry for bin/run.sh is mode 0644
	badFileBytes    bool   // tarball carries bytes the file manifest does not list
	traversal       bool   // tarball holds a ../ entry
	omitBundle      bool
	omitAttBundle   bool
	// v2Schema, when set, also writes <tarball>.attest.v2.bundle carrying that
	// schema_version, and makes the plain .attest.bundle an older-format one
	// with no dsseEnvelope (what the published releases carry).
	v2Schema string
	// v2Only writes the v2 bundle and no plain .attest.bundle at all.
	v2Only bool
}

// buildRelease writes a release folder for pack "demo" 1.0.0 into a temp dir.
func buildRelease(t *testing.T, o buildOpts) *fixture {
	t.Helper()
	dir := t.TempDir()
	if o.schema == "" {
		o.schema = "0.2.0"
	}
	files := map[string][]byte{
		"pack.yaml":         []byte("name: demo\nversion: 1.0.0\n"),
		"doc.md":            []byte("hello\n"),
		"bin/run.sh":        []byte("#!/bin/sh\necho hi\n"),
		"exec-manifest.txt": []byte("bin/run.sh\n"),
	}
	var csv strings.Builder
	names := []string{"bin/run.sh", "doc.md", "exec-manifest.txt", "pack.yaml"}
	for _, n := range names {
		fmt.Fprintf(&csv, "%s,%d,%s\n", hexSHA(files[n]), len(files[n]), n)
	}
	files["file-manifest.csv"] = []byte(csv.String())

	tarPath := filepath.Join(dir, "demo-1.0.0-arcaven.tar.gz")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	order := append(append([]string{}, names...), "file-manifest.csv")
	for _, n := range order {
		body := files[n]
		mode := int64(0o644)
		if n == "bin/run.sh" && !o.loseExecBit {
			mode = 0o755
		}
		if n == "doc.md" && o.badFileBytes {
			body = []byte("tampered\n")
		}
		if err := tw.WriteHeader(&tar.Header{Name: "demo-1.0.0/" + n, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if o.traversal {
		evil := []byte("x")
		if err := tw.WriteHeader(&tar.Header{Name: "demo-1.0.0/../../evil", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(evil)
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
	tarBytes, _ := os.ReadFile(tarPath)

	art := map[string]any{"tarball": filepath.Base(tarPath), "tarball_sha256": hexSHA(tarBytes)}
	if !o.omitManifestSHA {
		art["file_manifest_sha256"] = hexSHA(files["file-manifest.csv"])
		art["exec_manifest_sha256"] = hexSHA(files["exec-manifest.txt"])
	}
	mkBundle := func(schema string) []byte {
		meta := map[string]any{"schema_version": schema, "pack": map[string]any{"name": "demo", "version": "1.0.0"}, "artifact": art}
		stmt, _ := json.Marshal(map[string]any{"_type": "https://in-toto.io/Statement/v0.1", "predicate": meta})
		b, _ := json.Marshal(map[string]any{"dsseEnvelope": map[string]any{"payload": base64.StdEncoding.EncodeToString(stmt)}})
		return b
	}
	bundle := mkBundle(o.schema)

	write := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !o.omitBundle {
		write("demo-1.0.0-arcaven.tar.gz.bundle", []byte("{}"))
	}
	if o.v2Schema != "" || o.v2Only {
		schema := o.v2Schema
		if schema == "" {
			schema = o.schema
		}
		write("demo-1.0.0-arcaven.tar.gz.attest.v2.bundle", mkBundle(schema))
		if !o.v2Only {
			write("demo-1.0.0-arcaven.tar.gz.attest.bundle", []byte(`{"base64Signature":"x","cert":"y","rekorBundle":{}}`))
		}
	} else if !o.omitAttBundle {
		write("demo-1.0.0-arcaven.tar.gz.attest.bundle", bundle)
	}
	write("file-manifest.csv", files["file-manifest.csv"])
	write("exec-manifest.txt", files["exec-manifest.txt"])
	return &fixture{dir: dir, tarball: tarPath, tarSHA: hexSHA(tarBytes)}
}

// assertVerifierArgs checks that Verify handed the verifier the signature
// bundle, the attestation bundle and the tarball, each in its own slot.
func assertVerifierArgs(t *testing.T, v *fakeVerifier, fx *fixture) {
	t.Helper()
	wantBlob := [2]string{filepath.Join(fx.dir, "demo-1.0.0-arcaven.tar.gz.bundle"), fx.tarball}
	if len(v.blobArgs) != 1 || v.blobArgs[0] != wantBlob {
		t.Errorf("VerifyBlob args = %v, want [%v]", v.blobArgs, wantBlob)
	}
	wantAtt := [3]string{filepath.Join(fx.dir, "demo-1.0.0-arcaven.tar.gz.attest.bundle"), fx.tarball, PredicateType}
	if len(v.attArgs) != 1 || v.attArgs[0] != wantAtt {
		t.Errorf("VerifyAttestation args = %v, want [%v]", v.attArgs, wantAtt)
	}
}

func mustFail(t *testing.T, res *Result, err error, want string) {
	t.Helper()
	if err == nil {
		if res != nil && res.Cleanup != nil {
			res.Cleanup()
		}
		t.Fatalf("Verify succeeded, want an error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err, want)
	}
	if res != nil && res.Root != "" {
		t.Fatalf("a refused release returned a root: %s", res.Root)
	}
}

func TestVerify_ValidReleaseExtractsAndRecordsDigest(t *testing.T) {
	fx := buildRelease(t, buildOpts{})
	v := &fakeVerifier{}
	res, err := Verify(fx.dir, "demo", v, Options{})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	defer res.Cleanup()
	if _, err := os.Stat(filepath.Join(res.Root, "pack.yaml")); err != nil {
		t.Errorf("pack.yaml not in the extracted root %s: %v", res.Root, err)
	}
	if res.TarballSHA256 != fx.tarSHA {
		t.Errorf("TarballSHA256 = %q, want %q", res.TarballSHA256, fx.tarSHA)
	}
	if res.SchemaVersion != "0.2.0" {
		t.Errorf("SchemaVersion = %q, want 0.2.0", res.SchemaVersion)
	}
	if got := strings.Join(v.calls, ","); got != "blob,attest" {
		t.Errorf("verifier calls = %s, want blob,attest (signature, then attestation, per docs/release-url-format.md steps 1 and 2)", got)
	}
	assertVerifierArgs(t, v, fx)
	if len(v.attTypes) != 1 || v.attTypes[0] != PredicateType {
		t.Errorf("attestation types tried = %v, want [%s]", v.attTypes, PredicateType)
	}
}

func TestVerify_TarballAcceptedByPath(t *testing.T) {
	fx := buildRelease(t, buildOpts{})
	v := &fakeVerifier{}
	res, err := Verify(fx.tarball, "demo", v, Options{})
	if err != nil {
		t.Fatalf("Verify by tarball path: %v", err)
	}
	res.Cleanup()
	assertVerifierArgs(t, v, fx)
}

func TestVerify_RefusesATamperedTarball(t *testing.T) {
	fx := buildRelease(t, buildOpts{})
	b, _ := os.ReadFile(fx.tarball)
	b = append(b, 0)
	if err := os.WriteFile(fx.tarball, b, 0o644); err != nil {
		t.Fatal(err)
	}
	// The fake verifier cannot see the change; the attested digest must.
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
	mustFail(t, res, err, "tarball_sha256")
}

func TestVerify_RefusesAManifestThatDiffersFromTheAttestedHash(t *testing.T) {
	fx := buildRelease(t, buildOpts{})
	if err := os.WriteFile(filepath.Join(fx.dir, "file-manifest.csv"), []byte("deadbeef,1,pack.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
	mustFail(t, res, err, "file_manifest_sha256")
}

func TestVerify_RefusesAnExecManifestThatDiffersFromTheAttestedHash(t *testing.T) {
	fx := buildRelease(t, buildOpts{})
	if err := os.WriteFile(filepath.Join(fx.dir, "exec-manifest.txt"), []byte("doc.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
	mustFail(t, res, err, "exec_manifest_sha256")
}

func TestVerify_RefusesAMissingSignatureBundle(t *testing.T) {
	fx := buildRelease(t, buildOpts{omitBundle: true})
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
	mustFail(t, res, err, "tar.gz.bundle")
}

func TestVerify_RefusesAMissingAttestationBundle(t *testing.T) {
	fx := buildRelease(t, buildOpts{omitAttBundle: true})
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
	mustFail(t, res, err, "attest.bundle")
}

func TestVerify_RefusesWhenTheSignatureFails(t *testing.T) {
	fx := buildRelease(t, buildOpts{})
	v := &fakeVerifier{blobErr: errors.New("none of the expected identities matched")}
	res, err := Verify(fx.dir, "demo", v, Options{})
	mustFail(t, res, err, "none of the expected identities matched")
}

func TestVerify_RefusesWhenTheAttestationFails(t *testing.T) {
	fx := buildRelease(t, buildOpts{})
	v := &fakeVerifier{attErr: errors.New("attestation signature invalid")}
	res, err := Verify(fx.dir, "demo", v, Options{})
	mustFail(t, res, err, "attestation signature invalid")
}

func TestVerify_FallsBackToTheLegacyPredicateType(t *testing.T) {
	fx := buildRelease(t, buildOpts{})
	v := &fakeVerifier{attFailTypes: map[string]bool{PredicateType: true}}
	res, err := Verify(fx.dir, "demo", v, Options{})
	if err != nil {
		t.Fatalf("a release signed with the legacy type was refused: %v", err)
	}
	res.Cleanup()
	if len(v.attTypes) != 2 || v.attTypes[1] != LegacyPredicateType {
		t.Errorf("attestation types tried = %v, want [%s %s]", v.attTypes, PredicateType, LegacyPredicateType)
	}
}

func TestVerify_ManifestHashesAbsentWarnsAndInstalls(t *testing.T) {
	fx := buildRelease(t, buildOpts{omitManifestSHA: true, schema: "0.1.1"})
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
	if err != nil {
		t.Fatalf("a release without attested manifest hashes was refused: %v", err)
	}
	defer res.Cleanup()
	if len(res.Warnings) == 0 || !strings.Contains(strings.Join(res.Warnings, "\n"), "manifest hash") {
		t.Errorf("warnings = %v, want one naming the unavailable manifest hash check", res.Warnings)
	}
}

func TestVerify_SchemaVersionPolicy(t *testing.T) {
	for _, ok := range []string{"0.1.0", "0.1.1", "0.2.0", "0.2.0-draft"} {
		fx := buildRelease(t, buildOpts{schema: ok})
		res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
		if err != nil {
			t.Errorf("schema %s refused: %v", ok, err)
			continue
		}
		res.Cleanup()
	}
	for _, bad := range []string{"1.2.0", "9.9.9", ""} {
		fx := buildRelease(t, buildOpts{})
		// Keep the whole predicate, change only schema_version, so a refusal
		// for any other reason cannot satisfy this case.
		path := filepath.Join(fx.dir, "demo-1.0.0-arcaven.tar.gz.attest.bundle")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var env struct {
			DSSE struct {
				Payload string `json:"payload"`
			} `json:"dsseEnvelope"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		payload, err := base64.StdEncoding.DecodeString(env.DSSE.Payload)
		if err != nil {
			t.Fatal(err)
		}
		var stmt map[string]any
		if err := json.Unmarshal(payload, &stmt); err != nil {
			t.Fatal(err)
		}
		stmt["predicate"].(map[string]any)["schema_version"] = bad
		stmtBytes, _ := json.Marshal(stmt)
		bundle, _ := json.Marshal(map[string]any{"dsseEnvelope": map[string]any{"payload": base64.StdEncoding.EncodeToString(stmtBytes)}})
		if err := os.WriteFile(path, bundle, 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
		mustFail(t, res, err, "schema_version")
		if bad != "" && err != nil && !strings.Contains(err.Error(), bad) {
			t.Errorf("schema %q: error %q does not name the bad value", bad, err)
		}
	}
}

// A refused release leaves no extraction directory behind. The fixtures are
// built before TMPDIR moves, so the scratch directory holds only what Verify
// extracts.
func TestVerify_RefusalRemovesTheExtractionDir(t *testing.T) {
	good := buildRelease(t, buildOpts{})
	bad := buildRelease(t, buildOpts{badFileBytes: true})
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	// Anchor (a): a valid run extracts under TMPDIR, so the emptiness check
	// below cannot pass for a green that extracts somewhere else.
	res, err := Verify(good.dir, "demo", &fakeVerifier{}, Options{})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !strings.HasPrefix(res.Root, tmp) {
		t.Errorf("extraction root %s is not under TMPDIR %s", res.Root, tmp)
	}
	res.Cleanup()

	// Anchor (b): the release folder is untouched by a refusal.
	names := func(dir string) []string {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range ents {
			out = append(out, e.Name())
		}
		return out
	}
	folderBefore := names(bad.dir)
	tmpBefore := names(tmp)

	res, err = Verify(bad.dir, "demo", &fakeVerifier{}, Options{})
	mustFail(t, res, err, "doc.md")
	if got := names(tmp); strings.Join(got, ",") != strings.Join(tmpBefore, ",") {
		t.Errorf("extraction temp dir left behind: entries %v before, %v after", tmpBefore, got)
	}
	if got := names(bad.dir); strings.Join(got, ",") != strings.Join(folderBefore, ",") {
		t.Errorf("release folder changed by a refusal: %v before, %v after", folderBefore, got)
	}
}

func TestVerify_RefusesAnExecBitTheManifestRequires(t *testing.T) {
	fx := buildRelease(t, buildOpts{loseExecBit: true})
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
	mustFail(t, res, err, "bin/run.sh")
}

func TestVerify_RefusesFileBytesTheFileManifestDoesNotList(t *testing.T) {
	fx := buildRelease(t, buildOpts{badFileBytes: true})
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
	mustFail(t, res, err, "doc.md")
}

func TestVerify_RefusesAPathThatEscapesTheExtractionDir(t *testing.T) {
	fx := buildRelease(t, buildOpts{traversal: true})
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
	mustFail(t, res, err, "escapes")
}

func TestVerify_ShaPin(t *testing.T) {
	fx := buildRelease(t, buildOpts{})
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{SHA256: strings.Repeat("0", 64)})
	mustFail(t, res, err, "pin")
	res, err = Verify(fx.dir, "demo", &fakeVerifier{}, Options{SHA256: fx.tarSHA})
	if err != nil {
		t.Fatalf("matching pin refused: %v", err)
	}
	res.Cleanup()
}

func TestLocate(t *testing.T) {
	fx := buildRelease(t, buildOpts{})
	if dir, tb, ok := Locate(fx.dir); !ok || dir != fx.dir || tb != fx.tarball {
		t.Errorf("Locate(release folder) = %q, %q, %v", dir, tb, ok)
	}
	if dir, tb, ok := Locate(fx.tarball); !ok || dir != fx.dir || tb != fx.tarball {
		t.Errorf("Locate(tarball) = %q, %q, %v", dir, tb, ok)
	}
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "pack.yaml"), []byte("name: demo\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := Locate(plain); ok {
		t.Error("Locate(plain pack dir) reported a release")
	}
}

func TestNewCosign_RefusesWhenTheBinaryIsAbsent(t *testing.T) {
	_, err := NewCosign(filepath.Join(t.TempDir(), "no-such-cosign"))
	if err == nil || !strings.Contains(err.Error(), "cosign") {
		t.Fatalf("NewCosign(absent) error = %v, want one naming cosign", err)
	}
}

func TestVerify_PrefersTheV2AttestationBundle(t *testing.T) {
	fx := buildRelease(t, buildOpts{v2Schema: "0.2.0"})
	v := &fakeVerifier{}
	res, err := Verify(fx.dir, "demo", v, Options{})
	if err != nil {
		t.Fatalf("a release with a v2 attestation bundle was refused: %v", err)
	}
	res.Cleanup()
	want := filepath.Join(fx.dir, "demo-1.0.0-arcaven.tar.gz.attest.v2.bundle")
	if len(v.attArgs) != 1 || v.attArgs[0][0] != want {
		t.Errorf("VerifyAttestation bundle args = %v, want the v2 bundle %s", v.attArgs, want)
	}
}

func TestVerify_ReadsTheProvenanceFromTheV2Bundle(t *testing.T) {
	// The v2 bundle carries a schema the policy refuses; the older bundle has
	// no payload at all. Only the v2 payload can be the one that is read.
	fx := buildRelease(t, buildOpts{v2Schema: "1.2.0"})
	res, err := Verify(fx.dir, "demo", &fakeVerifier{}, Options{})
	mustFail(t, res, err, "schema_version")
}

func TestVerify_AcceptsAReleaseWithOnlyAV2Bundle(t *testing.T) {
	fx := buildRelease(t, buildOpts{v2Only: true})
	v := &fakeVerifier{}
	res, err := Verify(fx.dir, "demo", v, Options{})
	if err != nil {
		t.Fatalf("a release with only a v2 attestation bundle was refused: %v", err)
	}
	res.Cleanup()
	if len(v.attArgs) != 1 || !strings.HasSuffix(v.attArgs[0][0], ".attest.v2.bundle") {
		t.Errorf("VerifyAttestation bundle args = %v, want the v2 bundle", v.attArgs)
	}
}

const (
	sanBuild     = "https://github.com/ArcavenAE/sideshow-packs/.github/workflows/build-pack.yml"
	sanReattest  = "https://github.com/ArcavenAE/sideshow-packs/.github/workflows/reattest-release.yml"
	sanOtherFile = "https://github.com/ArcavenAE/sideshow-packs/.github/workflows/intake-tests.yml"
	sanOtherRepo = "https://github.com/ArcavenAE/sideshow/.github/workflows/build-pack.yml"
	sanForkOwner = "https://github.com/someone/sideshow-packs/.github/workflows/build-pack.yml"
)

func TestIdentityPins(t *testing.T) {
	cases := []struct {
		name        string
		san         string
		wantAttest  bool
		wantBlobSig bool
	}{
		{"build-pack on main", sanBuild + "@refs/heads/main", true, true},
		{"reattest on main", sanReattest + "@refs/heads/main", true, false},
		{"build-pack at a pack version tag", sanBuild + "@refs/tags/bmad-v6.12.4-r3", false, true},
		{"build-pack at a vsdd-factory version tag", sanBuild + "@refs/tags/vsdd-factory-v1.0.0-rc.23", false, true},
		{"build-pack at a non-version tag", sanBuild + "@refs/tags/latest", false, false},
		{"build-pack at a tag with no pack", sanBuild + "@refs/tags/v6.12.4", false, false},
		{"build-pack at a version tag with a path after it", sanBuild + "@refs/tags/bmad-v6.12.4-r3/x", false, false},
		{"build-pack at a tag whose version is not a number", sanBuild + "@refs/tags/bmad-vX", false, false},
		// Director ruling 01M8FTTCS3: the trust boundary is the repository plus
		// build-pack.yml, which the SAN already fixes, so any pack-shaped name is
		// accepted at a version tag.
		{"build-pack at a pack-shaped tag with a bare major version", sanBuild + "@refs/tags/junk-v1", false, true},
		{"build-pack at a plain release version tag", sanBuild + "@refs/tags/bmad-v6.12.4", false, true},
		{"build-pack at a wardrobe version tag", sanBuild + "@refs/tags/wardrobe-v0.1.0", false, true},
		{"build-pack on another branch", sanBuild + "@refs/heads/feature", false, false},
		{"build-pack on a main-prefixed branch", sanBuild + "@refs/heads/main-2", false, false},
		{"build-pack on a pull request ref", sanBuild + "@refs/pull/7/merge", false, false},
		{"reattest at a tag", sanReattest + "@refs/tags/bmad-v6.12.4-r3", false, false},
		{"reattest on another branch", sanReattest + "@refs/heads/feature", false, false},
		{"another workflow file on main", sanOtherFile + "@refs/heads/main", false, false},
		{"another repository on main", sanOtherRepo + "@refs/heads/main", false, false},
		{"a fork owner on main", sanForkOwner + "@refs/heads/main", false, false},
		{"an unanchored suffix on the SAN", sanBuild + "@refs/heads/main/../x", false, false},
		{"a prefix before the SAN", "x" + sanBuild + "@refs/heads/main", false, false},
	}
	attest := regexp.MustCompile(AttestIdentityRegexp)
	blob := regexp.MustCompile(BlobIdentityRegexp)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if AttestIdentityRegexp == "" || BlobIdentityRegexp == "" {
				t.Fatal("an identity pin is empty")
			}
			if got := attest.MatchString(c.san); got != c.wantAttest {
				t.Errorf("attestation pin on %q = %v, want %v", c.san, got, c.wantAttest)
			}
			if got := blob.MatchString(c.san); got != c.wantBlobSig {
				t.Errorf("blob signature pin on %q = %v, want %v", c.san, got, c.wantBlobSig)
			}
		})
	}
}

func TestVerify_AV2FailureDoesNotFallBackToTheOlderBundle(t *testing.T) {
	fx := buildRelease(t, buildOpts{v2Schema: "0.2.0"})
	v := &fakeVerifier{attErr: errors.New("v2 attestation signature invalid")}
	res, err := Verify(fx.dir, "demo", v, Options{})
	mustFail(t, res, err, "v2 attestation signature invalid")
	old := filepath.Join(fx.dir, "demo-1.0.0-arcaven.tar.gz.attest.bundle")
	for _, a := range v.attArgs {
		if a[0] == old {
			t.Errorf("VerifyAttestation was called on the older bundle %s after the v2 bundle failed", old)
		}
	}
}

// writeRecorder writes a script that appends its argv, one argument per line,
// to a file named for its first argument, and exits 0.
func writeRecorder(t *testing.T) (script, dir string) {
	t.Helper()
	dir = t.TempDir()
	script = filepath.Join(dir, "cosign")
	body := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > \"" + dir + "/argv-$1.txt\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, dir
}

func argvAfter(t *testing.T, dir, sub, flag string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "argv-"+sub+".txt"))
	if err != nil {
		t.Fatalf("cosign %s was not run: %v", sub, err)
	}
	args := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("cosign %s argv %q has no %s", sub, args, flag)
	return ""
}

func TestNewCosign_PassesEachCommandItsOwnIdentityPin(t *testing.T) {
	script, dir := writeRecorder(t)
	c, err := NewCosign(script)
	if err != nil {
		t.Fatalf("NewCosign(%s): %v", script, err)
	}
	if err := c.VerifyBlob("b.bundle", "a.tar.gz"); err != nil {
		t.Fatalf("VerifyBlob: %v", err)
	}
	if err := c.VerifyAttestation("b.attest.v2.bundle", "a.tar.gz", PredicateType); err != nil {
		t.Fatalf("VerifyAttestation: %v", err)
	}
	if got := argvAfter(t, dir, "verify-blob", "--certificate-identity-regexp"); got != BlobIdentityRegexp {
		t.Errorf("verify-blob identity pin = %q, want BlobIdentityRegexp %q", got, BlobIdentityRegexp)
	}
	if got := argvAfter(t, dir, "verify-blob-attestation", "--certificate-identity-regexp"); got != AttestIdentityRegexp {
		t.Errorf("verify-blob-attestation identity pin = %q, want AttestIdentityRegexp %q", got, AttestIdentityRegexp)
	}
	if got := argvAfter(t, dir, "verify-blob-attestation", "--type"); got != PredicateType {
		t.Errorf("verify-blob-attestation --type = %q, want %q", got, PredicateType)
	}
	if got := argvAfter(t, dir, "verify-blob", "--certificate-oidc-issuer"); got != "https://token.actions.githubusercontent.com" {
		t.Errorf("verify-blob issuer = %q", got)
	}
}
