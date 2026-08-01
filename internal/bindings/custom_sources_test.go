package bindings

import "testing"

func TestUnregisterCustomSource(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	project := t.TempDir()
	other := t.TempDir()
	for _, p := range []string{project, other} {
		if _, err := RegisterCustomSource(p, "bmad"); err != nil {
			t.Fatalf("register %s: %v", p, err)
		}
	}

	removed, err := UnregisterCustomSource(project, "bmad")
	if err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if !removed {
		t.Fatal("registered pair must report removed")
	}
	sources, err := ListCustomSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Project != other {
		t.Errorf("sources after unregister = %+v, want only %s", sources, other)
	}

	// Second unregister of the same pair is a clean no-op.
	removed, err = UnregisterCustomSource(project, "bmad")
	if err != nil {
		t.Fatalf("second unregister: %v", err)
	}
	if removed {
		t.Error("already-absent pair must report not removed")
	}

	// Pack mismatch does not remove.
	removed, err = UnregisterCustomSource(other, "spectacle")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("pack-mismatched pair must not remove")
	}
}
