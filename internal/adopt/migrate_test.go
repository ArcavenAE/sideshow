package adopt

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/ledger"
)

var migrateNow = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

// migrateScene stands up a machine with a user-scope (machine-wide)
// enable of the foreign channel and four repos around it:
//
//	target:      depends on the machine-wide enable; the adoption target
//	dependent:   depends on it too, and is in the sideshow ledger
//	independent: carries its own project-scope enable
//	suppressed:  overrides the machine-wide enable to false
//
// The migration must pin exactly the first two and leave the other two
// alone, because those two are the only ones whose effective enablement
// would change when the machine-wide entry goes.
type migrateScene struct {
	opts        MigrateOptions
	configDir   string
	target      string
	dependent   string
	independent string
	suppressed  string
}

func newMigrateScene(t *testing.T) *migrateScene {
	t.Helper()
	configDir := t.TempDir()
	cache := filepath.Join(t.TempDir(), "cache", "vsdd-factory")
	writeTree(t, cache, platform(t))
	mustWrite(t, configDir, "plugins/installed_plugins.json", `{"version": 2, "plugins": {"vsdd-factory@claude-mp": [
	  {"scope": "user", "installPath": "`+cache+`", "version": "1.0.0-rc.23"}
	]}}`, 0o644)
	mustWrite(t, configDir, "settings.json", `{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`, 0o644)

	s := &migrateScene{
		configDir:   configDir,
		target:      t.TempDir(),
		dependent:   t.TempDir(),
		independent: t.TempDir(),
		suppressed:  t.TempDir(),
	}
	mustWrite(t, s.independent, ".claude/settings.json",
		`{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`, 0o644)
	mustWrite(t, s.suppressed, ".claude/settings.local.json",
		`{"enabledPlugins": {"vsdd-factory@claude-mp": false}}`, 0o644)

	// The ledger names the dependent repo; the others arrive by flag.
	ledgerPath := filepath.Join(t.TempDir(), "repo-bindings.yaml")
	led := &ledger.Ledger{}
	if err := led.SetRow(s.dependent, "other-pack", ledger.Row{Version: "1.0.0", Channel: "sideshow-native"}); err != nil {
		t.Fatal(err)
	}
	if err := led.Save(ledgerPath); err != nil {
		t.Fatal(err)
	}

	s.opts = MigrateOptions{
		RepoDir:    s.target,
		Pack:       "vsdd-factory",
		ConfigDir:  configDir,
		LedgerPath: ledgerPath,
		AlsoRepos:  []string{s.independent, s.suppressed},
		Now:        migrateNow,
	}
	return s
}

// effective reports the pack identities a session in repo would load.
func (s *migrateScene) effective(t *testing.T, repo string) []string {
	t.Helper()
	census, err := foreign.TakeCensus(s.configDir, "vsdd-factory")
	if err != nil {
		t.Fatal(err)
	}
	view, err := census.ResolveRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	return view.EffectivelyEnabled
}

func (s *migrateScene) planFor(t *testing.T, out *MigrationOutcome, repo string) RepoPlan {
	t.Helper()
	for _, p := range out.Repos {
		if p.Dir == repo {
			return p
		}
	}
	t.Fatalf("%s is not in the sweep set: %+v", repo, out.Repos)
	return RepoPlan{}
}

func TestMigrateUserScope_PinsOnlyDependentRepos(t *testing.T) {
	t.Parallel()
	s := newMigrateScene(t)

	pre := map[string][]string{}
	for _, repo := range []string{s.target, s.dependent, s.independent, s.suppressed} {
		pre[repo] = s.effective(t, repo)
	}

	s.opts.Consented = true
	out, err := MigrateUserScope(s.opts)
	if err != nil {
		t.Fatalf("MigrateUserScope: %v", err)
	}
	if !out.Applied {
		t.Fatal("migration reported not applied")
	}
	if out.Pinned != 2 {
		t.Errorf("pinned %d repos, want 2 (target + dependent)", out.Pinned)
	}

	tests := []struct {
		name    string
		repo    string
		wantPin bool
	}{
		{"adoption target", s.target, true},
		{"ledger repo on the machine-wide enable", s.dependent, true},
		{"repo enabled at project scope", s.independent, false},
		{"repo that suppresses the identity", s.suppressed, false},
	}
	for _, tc := range tests {
		plan := s.planFor(t, out, tc.repo)
		if got := len(plan.Pin) > 0; got != tc.wantPin {
			t.Errorf("%s: pinned = %v, want %v (plan %+v)", tc.name, got, tc.wantPin, plan)
		}
		if got := s.effective(t, tc.repo); strings.Join(got, ",") != strings.Join(pre[tc.repo], ",") {
			t.Errorf("%s: effective enablement changed from %v to %v", tc.name, pre[tc.repo], got)
		}
	}

	// The machine-wide entry is gone, and nothing else in the file is.
	census, err := foreign.TakeCensus(s.configDir, "vsdd-factory")
	if err != nil {
		t.Fatal(err)
	}
	if ids := census.UserEnabledIdentities(); len(ids) != 0 {
		t.Errorf("machine-wide enable survived: %v", ids)
	}
}

// TestMigrateUserScope_TargetIsAdoptableAfterwards is the clause the
// migration exists for: adopt refuses against a user-scope enable, and
// this is what makes the refusal answerable.
func TestMigrateUserScope_TargetIsAdoptableAfterwards(t *testing.T) {
	t.Parallel()
	s := newMigrateScene(t)

	adoptOpts := Options{
		RepoDir: s.target, Pack: "vsdd-factory",
		LedgerPath: filepath.Join(t.TempDir(), "repo-bindings.yaml"),
		ConfigDir:  s.configDir,
		StoreRoot:  storeTree(t),
		Prefix:     "vsdd",
		BoundDir:   filepath.Join(t.TempDir(), "bound"),
		Now:        migrateNow,
	}
	if _, err := Adopt(adoptOpts); err == nil || !strings.Contains(err.Error(), "USER scope") {
		t.Fatalf("Adopt before migration = %v, want the user-scope refusal", err)
	}

	s.opts.Consented = true
	if _, err := MigrateUserScope(s.opts); err != nil {
		t.Fatalf("MigrateUserScope: %v", err)
	}

	out, err := Adopt(adoptOpts)
	if err != nil {
		t.Fatalf("Adopt after migration: %v", err)
	}
	if !out.Suppressed {
		t.Error("adoption did not suppress the foreign identity")
	}
	if len(out.FootprintDrift) != 0 {
		t.Errorf("adoption changed machine-level state: %v", out.FootprintDrift)
	}
}

func TestMigrateUserScope_DryRunWritesNothing(t *testing.T) {
	t.Parallel()
	s := newMigrateScene(t)
	s.opts.DryRun = true

	before := map[string]map[string]string{}
	for _, dir := range []string{s.configDir, s.target, s.dependent, s.independent, s.suppressed} {
		before[dir] = snapshotDir(t, dir)
	}

	out, err := MigrateUserScope(s.opts)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if out.Applied {
		t.Error("dry run reported the migration applied")
	}
	if out.Pinned != 2 {
		t.Errorf("dry run planned %d pins, want 2", out.Pinned)
	}
	for dir, snap := range before {
		if diff := diffSnapshots(snap, snapshotDir(t, dir)); len(diff) != 0 {
			t.Errorf("dry run wrote into %s: %v", dir, diff)
		}
	}
}

func TestMigrateUserScope_RefusesWithoutConsent(t *testing.T) {
	t.Parallel()
	s := newMigrateScene(t)
	before := snapshotDir(t, s.target)

	_, err := MigrateUserScope(s.opts)
	if err == nil || !strings.Contains(err.Error(), "needs consent") {
		t.Fatalf("MigrateUserScope = %v, want a consent refusal", err)
	}
	if diff := diffSnapshots(before, snapshotDir(t, s.target)); len(diff) != 0 {
		t.Errorf("unconsented run wrote into the repo: %v", diff)
	}
}

func TestMigrateUserScope_Refusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(t *testing.T, s *migrateScene)
		wantErr string
	}{
		{
			name: "no machine-wide enable to migrate",
			mutate: func(t *testing.T, s *migrateScene) {
				mustWrite(t, s.configDir, "settings.json", `{"enabledPlugins": {}}`, 0o644)
			},
			wantErr: "no user-scope enable",
		},
		{
			name:    "project scope without commit consent",
			mutate:  func(t *testing.T, s *migrateScene) { s.opts.Scope = foreign.ScopeProject },
			wantErr: "--commit-consent",
		},
		{
			name: "unknown scope",
			mutate: func(t *testing.T, s *migrateScene) {
				s.opts.Scope = foreign.ScopeUser
			},
			wantErr: "not \"user\"",
		},
		{
			name: "a factory run in flight anywhere in the sweep",
			mutate: func(t *testing.T, s *migrateScene) {
				// The lock is in a swept repo that is NOT the target, so
				// this also pins that the sweep reaches past the target.
				mustWrite(t, s.dependent, ".factory/STATE.md",
					"---\nfactory_lock:\n  holder: dev@example.com\n  locked_at: "+
						migrateNow.Add(-10*time.Minute).Format(time.RFC3339)+
						"\n  expires_at: "+migrateNow.Add(30*time.Minute).Format(time.RFC3339)+
						"\n---\n\n# Factory State\n", 0o644)
			},
			wantErr: "blocker",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newMigrateScene(t)
			s.opts.Consented = true
			tc.mutate(t, s)
			before := snapshotDir(t, s.configDir)

			_, err := MigrateUserScope(s.opts)
			if err == nil {
				t.Fatalf("MigrateUserScope succeeded, want a refusal naming %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("MigrateUserScope = %v, want it to name %q", err, tc.wantErr)
			}
			if diff := diffSnapshots(before, snapshotDir(t, s.configDir)); len(diff) != 0 {
				t.Errorf("refusal changed machine state: %v", diff)
			}
		})
	}
}

// TestMigrateUserScope_DryRunPredictsAnUnwritableSettingsFile is the
// F099-a lesson applied to this verb: a dry run validates every step of
// the plan it prints, so an unwritable settings file is reported before
// the operator consents rather than discovered halfway through.
func TestMigrateUserScope_DryRunPredictsAnUnwritableSettingsFile(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}
	s := newMigrateScene(t)
	s.opts.DryRun = true

	// The dependent repo gets a settings file nobody can write.
	mustWrite(t, s.dependent, ".claude/settings.local.json", `{}`, 0o400)
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(s.dependent, ".claude", "settings.local.json"), 0o644)
	})

	out, err := MigrateUserScope(s.opts)
	if err == nil {
		t.Fatal("dry run promised success for a plan whose pin cannot be written")
	}
	if !strings.Contains(err.Error(), "blocker") {
		t.Errorf("dry run error = %v, want it to report blockers", err)
	}
	var found bool
	for _, b := range out.Blockers {
		if strings.Contains(b, s.dependent) && strings.Contains(b, "not writable") {
			found = true
		}
	}
	if !found {
		t.Errorf("blockers do not name the unwritable pin: %v", out.Blockers)
	}
}

func TestMigrateUserScope_ProjectScopeWritesTheTrackedFile(t *testing.T) {
	t.Parallel()
	s := newMigrateScene(t)
	s.opts.Scope = foreign.ScopeProject
	s.opts.CommitConsent = true
	s.opts.Consented = true

	if _, err := MigrateUserScope(s.opts); err != nil {
		t.Fatalf("MigrateUserScope: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(s.target, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("project-scope pin not written: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	enables, _ := settings["enabledPlugins"].(map[string]any)
	if v, ok := enables["vsdd-factory@claude-mp"].(bool); !ok || !v {
		t.Errorf("project-scope pin = %v, want true", enables)
	}
	if _, err := os.Stat(filepath.Join(s.target, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Error("project scope also wrote the local settings file")
	}
}

func TestUndoLog_RestoresPriorState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.json")
	if err := os.WriteFile(existing, []byte(`{"enabledPlugins": {"a@mp": true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "fresh", "settings.local.json")

	prior, err := foreign.ReadEnable(existing, "a@mp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.SetEnable(existing, "a@mp", false); err != nil {
		t.Fatal(err)
	}
	created, err := foreign.SetEnable(fresh, "b@mp", true)
	if err != nil {
		t.Fatal(err)
	}

	var undo undoLog
	undo.add(existing, "a@mp", prior, false)
	undo.add(fresh, "b@mp", foreign.EnableEntry{Path: fresh}, created)
	undo.run()

	restored, err := foreign.ReadEnable(existing, "a@mp")
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Present || !restored.Value {
		t.Errorf("prior value not restored: %+v", restored)
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Errorf("file sideshow created was not removed: %v", err)
	}
}

func TestFindGitCheckouts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, rel := range []string{
		"orc/repo-a/.git/HEAD",
		"orc/repo-b/.git/HEAD",
		"orc/repo-b/nested/.git/HEAD", // not descended into
		"orc/notarepo/README.md",
		".hidden/repo-c/.git/HEAD", // hidden roots are skipped
	} {
		mustWrite(t, root, rel, "ref: refs/heads/main\n", 0o644)
	}

	found, err := findGitCheckouts(root, 3)
	if err != nil {
		t.Fatalf("findGitCheckouts: %v", err)
	}
	want := []string{filepath.Join(root, "orc", "repo-a"), filepath.Join(root, "orc", "repo-b")}
	if strings.Join(found, ",") != strings.Join(want, ",") {
		t.Errorf("findGitCheckouts = %v, want %v", found, want)
	}

	if _, err := findGitCheckouts(filepath.Join(root, "orc", "notarepo", "README.md"), 3); err == nil {
		t.Error("findGitCheckouts accepted a file as a sweep root")
	}
}

func TestReportMarketplaces(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		registry string
		want     string
		notWant  string
	}{
		{
			name:     "sole plugin: removal is decided safe",
			registry: `{"version": 2, "plugins": {"vsdd-factory@claude-mp": [{"scope": "user"}]}}`,
			want:     "removal is safe",
			notWant:  "KEEP",
		},
		{
			name: "shared marketplace: keep it, and say what would break",
			registry: `{"version": 2, "plugins": {
			  "vsdd-factory@claude-mp": [{"scope": "user"}],
			  "other-pack@claude-mp": [{"scope": "user"}]
			}}`,
			want:    "KEEP",
			notWant: "removal is safe",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configDir := t.TempDir()
			mustWrite(t, configDir, "plugins/installed_plugins.json", tc.registry, 0o644)
			census, err := foreign.TakeCensus(configDir, "vsdd-factory")
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			reportMarketplaces(&buf, census)
			if !strings.Contains(buf.String(), tc.want) {
				t.Errorf("report missing %q:\n%s", tc.want, buf.String())
			}
			if strings.Contains(buf.String(), tc.notWant) {
				t.Errorf("report contains %q it should not:\n%s", tc.notWant, buf.String())
			}
		})
	}
}

// storeTree lays down a sideshow store version dir matching the foreign
// cache tree, so an adoption run after a migration has something to
// enable.
func storeTree(t *testing.T) string {
	t.Helper()
	store := filepath.Join(t.TempDir(), "1.0.0-rc.23")
	writeTree(t, store, platform(t))
	return store
}
