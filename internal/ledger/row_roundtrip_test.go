package ledger

import (
	"path/filepath"
	"testing"
)

// settings_restored_sha survives Save and Load, and is omitted when empty.
func TestRow_SettingsRestoredSHARoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo-bindings.yaml")
	l := &Ledger{}
	if err := l.SetRow("/r", "p", Row{Version: "1", Artifacts: []string{"agent-file:a"}, SettingsRestoredSHA: "abc123"}); err != nil {
		t.Fatal(err)
	}
	if err := l.SetRow("/r", "q", Row{Version: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if r := got.RepoRow("/r", "p"); r == nil || r.SettingsRestoredSHA != "abc123" {
		t.Errorf("row p = %+v, want settings_restored_sha abc123", r)
	}
	if r := got.RepoRow("/r", "q"); r == nil || r.SettingsRestoredSHA != "" {
		t.Errorf("row q = %+v, want no field", r)
	}
}
