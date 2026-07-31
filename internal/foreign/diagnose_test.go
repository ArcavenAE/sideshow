package foreign

import (
	"strings"
	"testing"
)

func findingCodes(fs []Finding) map[string]Severity {
	out := map[string]Severity{}
	for _, f := range fs {
		out[f.Code] = f.Severity
	}
	return out
}

func TestDiagnose_Severities(t *testing.T) {
	c := &Census{
		Pack: "vsdd-factory",
		Installs: []Install{
			{Identity: "vsdd-factory@claude-mp", Scope: "user"},
		},
		userEnables: map[string]enableEntry{
			"vsdd-factory@claude-mp": {value: true, path: "/cfg/settings.json"},
		},
		installed: map[string]bool{"vsdd-factory@claude-mp": true},
	}

	tests := []struct {
		name            string
		view            *RepoView
		sideshowActive  bool
		perRepoRequired bool
		want            map[string]Severity
		absent          []string
	}{
		{
			name:            "machine coexistence alone is INFO plus the user-scope ERROR",
			view:            &RepoView{RepoDir: "/r"},
			sideshowActive:  true,
			perRepoRequired: true,
			want:            map[string]Severity{"dual-channel-machine": Info, "user-scope-enable": Error},
			absent:          []string{"same-repo-double-dispatch"},
		},
		{
			name:            "same-repo double dispatch is ERROR",
			view:            &RepoView{RepoDir: "/r", EffectivelyEnabled: []string{"vsdd-factory@claude-mp"}},
			sideshowActive:  true,
			perRepoRequired: true,
			want:            map[string]Severity{"same-repo-double-dispatch": Error},
		},
		{
			name:            "foreign enable without sideshow bindings raises no dispatch error",
			view:            &RepoView{RepoDir: "/r", EffectivelyEnabled: []string{"vsdd-factory@claude-mp"}},
			sideshowActive:  false,
			perRepoRequired: true,
			absent:          []string{"same-repo-double-dispatch", "dual-channel-machine"},
		},
		{
			name:            "user-scope enable is fine for packs without the per-repo mandate",
			view:            &RepoView{RepoDir: "/r"},
			sideshowActive:  false,
			perRepoRequired: false,
			absent:          []string{"user-scope-enable"},
		},
		{
			name: "orphaned enable is WARN",
			view: &RepoView{RepoDir: "/r", Orphans: []OrphanedEnable{
				{Identity: "vsdd-factory@ghost-mp", Scope: "user", Enabled: true, Path: "/cfg/settings.json"},
			}},
			want: map[string]Severity{"orphaned-enable": Warn},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findingCodes(Diagnose(c, tt.view, tt.sideshowActive, tt.perRepoRequired))
			for code, sev := range tt.want {
				if got[code] != sev {
					t.Errorf("finding %s = %q, want %q (all: %v)", code, got[code], sev, got)
				}
			}
			for _, code := range tt.absent {
				if _, ok := got[code]; ok {
					t.Errorf("finding %s present, want absent (all: %v)", code, got)
				}
			}
		})
	}
}

func TestRefusalOptions_OffersNeverActs(t *testing.T) {
	msg := RefusalOptions("vsdd-factory@claude-mp", "/some/repo")
	for _, want := range []string{"adopt", "settings.local.json", "Abort", "never auto-"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal message missing %q:\n%s", want, msg)
		}
	}
}
