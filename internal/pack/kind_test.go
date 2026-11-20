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
