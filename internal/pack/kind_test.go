package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeKindPack writes a native pack source whose pack.yaml declares the given
// name and kind (an empty kind writes no kind line).
func writeKindPack(t *testing.T, name, kind string) string {
	t.Helper()
	src := t.TempDir()
	content := "name: " + name + "\nversion: 0.1.0\nschema_version: 0.1.0\n"
	if kind != "" {
		content += "kind: " + kind + "\n"
	}
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "site.toml"), []byte("# site values\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

func storeHolds(name string) bool {
	_, err := os.Stat(filepath.Join(PacksDir(), name))
	return err == nil
}

// Today sideshow reads no kind from pack.yaml. The change is the refusal only:
// an overlay, a pack and a pack with no kind all install as before.
func TestInstall_KindOverlayAndPackAreAccepted(t *testing.T) {
	for _, kind := range []string{"overlay", "pack", ""} {
		freezeSafeHome(t)
		src := writeKindPack(t, "arcaven-site", kind)
		if err := InstallFromLocal("arcaven-site", src, true); err != nil {
			t.Errorf("kind %q: install refused: %v", kind, err)
		}
		if !storeHolds("arcaven-site") {
			t.Errorf("kind %q: nothing in the store after a successful install", kind)
		}
	}
}

// An unknown kind is refused at install, naming the value and the two allowed
// kinds, before anything is copied.
func TestInstall_UnknownKindIsRefusedBeforeCopy(t *testing.T) {
	freezeSafeHome(t)
	src := writeKindPack(t, "arcaven-site", "widget")
	err := InstallFromLocal("arcaven-site", src, true)
	if err == nil {
		t.Fatal("a pack with kind: widget installed; want a refusal")
	}
	for _, want := range []string{"widget", "pack", "overlay"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err.Error(), want)
		}
	}
	if storeHolds("arcaven-site") {
		t.Error("the store holds the pack after a refused install; the refusal must come before the copy")
	}
}

// An overlay is never the wardrobe contents: it is refused under the name
// wardrobe, before anything is copied.
func TestInstall_OverlayUnderTheNameWardrobeIsRefused(t *testing.T) {
	freezeSafeHome(t)
	src := writeKindPack(t, "wardrobe", "overlay")
	err := InstallFromLocal("wardrobe", src, true)
	if err == nil {
		t.Fatal("an overlay installed under the name wardrobe; want a refusal")
	}
	if !strings.Contains(err.Error(), "overlay") || !strings.Contains(err.Error(), "wardrobe") {
		t.Errorf("refusal %q does not name overlay and wardrobe", err.Error())
	}
	if storeHolds("wardrobe") {
		t.Error("the store holds wardrobe after a refused install")
	}
}

// writeBmadKindPack writes the installer layout: the pack identity sits in
// _bmad/pack.yaml and the manifest in _bmad/_config, so install strips _bmad/.
func writeBmadKindPack(t *testing.T, name, kind string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "_bmad", "_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "_bmad", "_config", "manifest.yaml"), []byte("installation:\n  version: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := "name: " + name + "\nversion: 0.1.0\nkind: " + kind + "\n"
	if err := os.WriteFile(filepath.Join(src, "_bmad", "pack.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

// The installer layout declares its identity in _bmad/pack.yaml, the second
// place ValidateName reads, so the kind there is checked too.
func TestInstall_KindInBmadPackYamlIsChecked(t *testing.T) {
	freezeSafeHome(t)
	err := InstallFromLocal("arcaven-site", writeBmadKindPack(t, "arcaven-site", "widget"), true)
	if err == nil || !strings.Contains(err.Error(), "widget") {
		t.Errorf("kind: widget in _bmad/pack.yaml: err = %v; want a refusal naming widget", err)
	}
	if storeHolds("arcaven-site") {
		t.Error("the store holds the pack after a refused install")
	}
	freezeSafeHome(t)
	err = InstallFromLocal("wardrobe", writeBmadKindPack(t, "wardrobe", "overlay"), true)
	if err == nil || !strings.Contains(err.Error(), "overlay") {
		t.Errorf("overlay under wardrobe in _bmad/pack.yaml: err = %v; want a refusal", err)
	}
	if storeHolds("wardrobe") {
		t.Error("the store holds wardrobe after a refused install")
	}
}

// A kind that does not decode as a string is refused, naming the file; it is
// never read as an absent kind.
func TestInstall_NonStringKindIsRefused(t *testing.T) {
	freezeSafeHome(t)
	src := t.TempDir()
	body := "name: arcaven-site\nversion: 0.1.0\nkind: [overlay]\n"
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := InstallFromLocal("arcaven-site", src, true)
	if err == nil {
		t.Fatal("a pack with kind: [overlay] installed; want a refusal")
	}
	if !strings.Contains(err.Error(), "pack.yaml") {
		t.Errorf("refusal %q does not name the file", err.Error())
	}
	if storeHolds("arcaven-site") {
		t.Error("the store holds the pack after a refused install")
	}
}

// A pack.yaml that exists but cannot be read is refused, naming the file. Only
// a not-exist error means there is no kind to check.
func TestInstall_UnreadablePackYamlIsRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a mode 000 file is still readable")
	}
	freezeSafeHome(t)
	src := writeKindPack(t, "arcaven-site", "overlay")
	packYAML := filepath.Join(src, "pack.yaml")
	if err := os.Chmod(packYAML, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(packYAML, 0o644) })

	err := InstallFromLocal("arcaven-site", src, true)
	if err == nil {
		t.Fatal("a pack with an unreadable pack.yaml installed; want a refusal")
	}
	if !strings.Contains(err.Error(), "pack.yaml") {
		t.Errorf("refusal %q does not name the file", err.Error())
	}
	if storeHolds("arcaven-site") {
		t.Error("the store holds the pack after a refused install")
	}
}
