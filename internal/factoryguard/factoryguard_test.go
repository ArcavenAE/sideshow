package factoryguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

// writeFactory builds a .factory fixture. stateFrontmatter is placed
// inside the --- fences; empty means no STATE.md at all.
func writeFactory(t *testing.T, stateFrontmatter string) string {
	t.Helper()
	repo := t.TempDir()
	factory := filepath.Join(repo, ".factory")
	if err := os.MkdirAll(factory, 0o755); err != nil {
		t.Fatal(err)
	}
	if stateFrontmatter != "" {
		content := "---\n" + stateFrontmatter + "---\n\n# Factory State\n"
		if err := os.WriteFile(filepath.Join(factory, "STATE.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func lockBlock(holder string, lockedAt, expiresAt time.Time) string {
	return "document_type: pipeline-state\n" +
		"timestamp: 2026-07-21T21:38:00Z\n" +
		"factory_lock:\n" +
		"  holder: " + holder + "\n" +
		"  locked_at: " + lockedAt.Format(time.RFC3339) + "\n" +
		"  expires_at: " + expiresAt.Format(time.RFC3339) + "\n" +
		"phase: E-21\n"
}

func TestCheckRepo_NoFactoryState(t *testing.T) {
	t.Parallel()
	v := CheckRepo(t.TempDir(), testNow)
	if v.InFlight() {
		t.Errorf("clean repo reported in flight: %+v", v.Signals)
	}
}

func TestCheckRepo_UnexpiredLockIsHard(t *testing.T) {
	t.Parallel()
	repo := writeFactory(t, lockBlock("dev@example.com", testNow.Add(-10*time.Minute), testNow.Add(35*time.Minute)))

	v := CheckRepo(repo, testNow)
	if !v.HardRefusal() {
		t.Fatalf("unexpired lock not a hard refusal: %+v", v.Signals)
	}
	msg := v.Refusal()
	if !strings.Contains(msg, "dev@example.com") {
		t.Errorf("refusal does not name the holder:\n%s", msg)
	}
	if !strings.Contains(msg, "no override") {
		t.Errorf("refusal does not state no-override:\n%s", msg)
	}
}

func TestCheckRepo_ExpiredLockIsOverridable(t *testing.T) {
	t.Parallel()
	repo := writeFactory(t, lockBlock("dev@example.com", testNow.Add(-3*time.Hour), testNow.Add(-2*time.Hour)))

	v := CheckRepo(repo, testNow)
	if !v.InFlight() || v.HardRefusal() {
		t.Fatalf("expired lock should be overridable in-flight: %+v", v.Signals)
	}
	msg := v.Refusal()
	if !strings.Contains(msg, "dev@example.com") || !strings.Contains(msg, "expired 2h0m0s ago") {
		t.Errorf("stale-lock signal missing holder or age:\n%s", msg)
	}
	if !strings.Contains(msg, "override") {
		t.Errorf("stale-lock signal does not mention the override:\n%s", msg)
	}
}

func TestCheckRepo_ClearedLockNoSignals(t *testing.T) {
	t.Parallel()
	// A paused factory: frontmatter with no factory_lock key, no
	// recent activity.
	repo := writeFactory(t, "document_type: pipeline-state\npipeline: PAUSED\n")

	v := CheckRepo(repo, testNow)
	if v.InFlight() {
		t.Errorf("paused factory reported in flight: %+v", v.Signals)
	}
}

func TestCheckRepo_RecentActivityIsSoftSignal(t *testing.T) {
	t.Parallel()
	repo := writeFactory(t, "pipeline: RUNNING\n")
	factory := filepath.Join(repo, ".factory")
	if err := os.WriteFile(filepath.Join(factory, "wave-state.yaml"), []byte("wave: W1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(factory, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "dispatcher-internal-2026-07-31.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Files just written: both activity signals fire against a "now"
	// close to wall clock.
	v := CheckRepo(repo, time.Now())
	kinds := map[string]bool{}
	for _, s := range v.Signals {
		kinds[s.Kind] = true
		if s.Hard {
			t.Errorf("activity signal %s marked hard; only the unexpired lock is", s.Kind)
		}
	}
	if !kinds[SignalWaveState] || !kinds[SignalBurstLog] {
		t.Errorf("expected wave-state and burst-log signals, got %+v", v.Signals)
	}

	// The same tree long after: signals age out.
	v = CheckRepo(repo, time.Now().Add(2*LockTTL))
	if v.InFlight() {
		t.Errorf("aged-out activity still in flight: %+v", v.Signals)
	}
}

func TestCheckRepo_MalformedLockFailsClosed(t *testing.T) {
	t.Parallel()
	repo := writeFactory(t, "factory_lock:\n  holder: dev@example.com\n  expires_at: not-a-time\n")

	v := CheckRepo(repo, testNow)
	if !v.InFlight() {
		t.Fatal("unreadable lock produced no signal; the guard must fail closed")
	}
	if v.HardRefusal() {
		t.Errorf("unreadable state should be overridable, not hard: %+v", v.Signals)
	}
	if v.Signals[0].Kind != SignalUnreadable {
		t.Errorf("signal kind = %s, want %s", v.Signals[0].Kind, SignalUnreadable)
	}
}

func TestCheckRepo_NoFrontmatterNoLock(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	factory := filepath.Join(repo, ".factory")
	if err := os.MkdirAll(factory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factory, "STATE.md"), []byte("# Just a heading\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := CheckRepo(repo, testNow); v.InFlight() {
		t.Errorf("frontmatter-less STATE.md reported in flight: %+v", v.Signals)
	}
}

func TestCheckRepos_SweepReturnsOnlyInFlight(t *testing.T) {
	t.Parallel()
	locked := writeFactory(t, lockBlock("a@example.com", testNow.Add(-time.Minute), testNow.Add(44*time.Minute)))
	clean := t.TempDir()
	paused := writeFactory(t, "pipeline: PAUSED\n")

	verdicts := CheckRepos([]string{locked, clean, paused}, testNow)
	if len(verdicts) != 1 || verdicts[0].RepoDir != locked {
		t.Errorf("sweep = %+v, want only the locked repo", verdicts)
	}
}

// The parser survives the shape of a REAL factory frontmatter: huge
// narrative fields with colons, quotes, and nested brackets around
// the lock block.
func TestReadFactoryLock_ToleratesNarrativeFrontmatter(t *testing.T) {
	t.Parallel()
	fm := "document_type: pipeline-state\n" +
		"last_amended: \"2026-07-21 (v6.17) — huge narrative: with [nested: things] and \\\"quotes\\\"; more text\"\n" +
		"factory_lock:\n" +
		"  holder: dev@example.com\n" +
		"  locked_at: 2026-07-31T11:00:00Z\n" +
		"  expires_at: 2026-07-31T11:45:00Z\n" +
		"current_step: \"D-chain cite D-874 (POLICY 16: grep -n \\\"^## D-\\\" | tail -3)\"\n"
	repo := writeFactory(t, fm)

	lock, err := readFactoryLock(filepath.Join(repo, ".factory", "STATE.md"))
	if err != nil {
		t.Fatalf("readFactoryLock: %v", err)
	}
	if lock == nil || lock.Holder != "dev@example.com" {
		t.Fatalf("lock = %+v", lock)
	}
	if !lock.ExpiresAt.Equal(time.Date(2026, 7, 31, 11, 45, 0, 0, time.UTC)) {
		t.Errorf("expires_at = %v", lock.ExpiresAt)
	}
}
