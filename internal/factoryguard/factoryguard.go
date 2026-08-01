// Package factoryguard is the running-factory refusal predicate
// (aae-orc-d3nq.40): every mutating sideshow verb (enable, disable,
// adopt, uninstall) asks it before touching a repo, and doctor reads
// it for reporting. It is strictly READ-ONLY — it inspects factory
// state, it never writes any.
//
// The class-1 rule it enforces (findings 093/094): sideshow verbs
// never touch running-factory state, and never yank the ground out
// from under an in-flight run. Signals, from the pack's own
// conventions:
//
//   - an unexpired factory_lock in .factory/STATE.md frontmatter
//     (bin/factory-lock-write.sh shape, TTL 2700s) is a HARD refusal:
//     the holder is named and there is no override;
//   - an expired lock is overridable (--override-stale-lock in the
//     verbs), reported with holder and age, so a crashed run cannot
//     block removal forever;
//   - between lock holds, recent wave-state.yaml or burst-log
//     activity marks the factory in flight (overridable, but the
//     verbs must say why they were stopped);
//   - unreadable factory state fails closed as an overridable signal:
//     the guard cannot prove the factory is NOT running.
package factoryguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LockTTL is the pack's non-configurable factory_lock TTL
// (bin/factory-lock-write.sh Invariant 2). It doubles as the
// "recent activity" window for the soft signals.
const LockTTL = 2700 * time.Second

// Signal kinds.
const (
	SignalFactoryLock = "factory-lock"       // unexpired lock: HARD
	SignalStaleLock   = "stale-factory-lock" // expired lock: overridable
	SignalWaveState   = "wave-state-activity"
	SignalBurstLog    = "recent-burst-log"
	SignalUnreadable  = "unreadable-factory-state"
)

// Signal is one observed in-flight indicator.
type Signal struct {
	Kind   string
	Detail string
	// Hard signals permit no override; everything else the verbs may
	// pass with an explicit operator override flag.
	Hard bool
}

// Verdict is the guard's answer for one repo.
type Verdict struct {
	RepoDir string
	Signals []Signal
}

// InFlight reports whether any signal fired.
func (v *Verdict) InFlight() bool { return len(v.Signals) > 0 }

// HardRefusal reports whether an un-overridable signal fired.
func (v *Verdict) HardRefusal() bool {
	for _, s := range v.Signals {
		if s.Hard {
			return true
		}
	}
	return false
}

// Refusal renders the verb-facing refusal text: every signal, one per
// line, hard ones first.
func (v *Verdict) Refusal() string {
	var hard, soft []string
	for _, s := range v.Signals {
		line := fmt.Sprintf("[%s] %s", s.Kind, s.Detail)
		if s.Hard {
			hard = append(hard, line)
		} else {
			soft = append(soft, line)
		}
	}
	return strings.Join(append(hard, soft...), "\n")
}

// LockInfo is the parsed factory_lock block.
type LockInfo struct {
	Holder    string
	LockedAt  time.Time
	ExpiresAt time.Time
}

// CheckRepo inspects one repo for an in-flight factory. now is
// injected so verbs and tests share one clock read.
func CheckRepo(repoDir string, now time.Time) *Verdict {
	v := &Verdict{RepoDir: repoDir}
	factory := filepath.Join(repoDir, ".factory")
	if _, err := os.Stat(factory); err != nil {
		return v // no factory state at all
	}

	statePath := filepath.Join(factory, "STATE.md")
	lock, err := readFactoryLock(statePath)
	switch {
	case err != nil:
		v.Signals = append(v.Signals, Signal{
			Kind:   SignalUnreadable,
			Detail: fmt.Sprintf("%s could not be read (%v); the guard cannot prove the factory is not running", statePath, err),
		})
	case lock != nil && now.Before(lock.ExpiresAt):
		v.Signals = append(v.Signals, Signal{
			Kind: SignalFactoryLock,
			Detail: fmt.Sprintf("factory_lock held by %s (locked %s, expires %s); no override — wait for the run or its lock expiry",
				lock.Holder, lock.LockedAt.Format(time.RFC3339), lock.ExpiresAt.Format(time.RFC3339)),
			Hard: true,
		})
	case lock != nil:
		v.Signals = append(v.Signals, Signal{
			Kind: SignalStaleLock,
			Detail: fmt.Sprintf("stale factory_lock held by %s, expired %s ago; pass the stale-lock override to proceed",
				lock.Holder, now.Sub(lock.ExpiresAt).Round(time.Second)),
		})
	}

	cutoff := now.Add(-LockTTL)
	if mt, ok := mtime(filepath.Join(factory, "wave-state.yaml")); ok && mt.After(cutoff) {
		v.Signals = append(v.Signals, Signal{
			Kind:   SignalWaveState,
			Detail: fmt.Sprintf("wave-state.yaml written %s ago; a wave appears in flight", now.Sub(mt).Round(time.Second)),
		})
	}
	if path, mt, ok := newestEntry(filepath.Join(factory, "logs")); ok && mt.After(cutoff) {
		v.Signals = append(v.Signals, Signal{
			Kind:   SignalBurstLog,
			Detail: fmt.Sprintf("burst log %s written %s ago; a burst appears in flight", filepath.Base(path), now.Sub(mt).Round(time.Second)),
		})
	}
	return v
}

// CheckRepos sweeps a repo list (machine-scoped operations check
// every ledger-known repo) and returns only in-flight verdicts.
func CheckRepos(repoDirs []string, now time.Time) []*Verdict {
	var out []*Verdict
	for _, dir := range repoDirs {
		if v := CheckRepo(dir, now); v.InFlight() {
			out = append(out, v)
		}
	}
	return out
}

// readFactoryLock parses the factory_lock block out of STATE.md's
// YAML frontmatter by line scan. The frontmatter in live factories
// carries multi-kilobyte narrative fields, so a full YAML unmarshal
// is deliberately avoided; the lock writer's shape is fixed (the key
// followed by two-space-indented holder/locked_at/expires_at).
// Missing file or absent key returns (nil, nil); a lock block whose
// timestamps do not parse is an error (the guard fails closed).
func readFactoryLock(statePath string) (*LockInfo, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return nil, nil // no frontmatter, no lock
	}

	var lock *LockInfo
	inLock := false
	for _, raw := range lines[1:] {
		line := strings.TrimRight(raw, "\r")
		if line == "---" {
			break
		}
		if strings.HasPrefix(line, "factory_lock:") {
			lock = &LockInfo{}
			inLock = true
			continue
		}
		if !inLock {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			inLock = false
			continue
		}
		key, val, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch key {
		case "holder":
			lock.Holder = val
		case "locked_at":
			t, err := time.Parse(time.RFC3339, val)
			if err != nil {
				return nil, fmt.Errorf("factory_lock.locked_at %q: %w", val, err)
			}
			lock.LockedAt = t
		case "expires_at":
			t, err := time.Parse(time.RFC3339, val)
			if err != nil {
				return nil, fmt.Errorf("factory_lock.expires_at %q: %w", val, err)
			}
			lock.ExpiresAt = t
		}
	}
	if lock != nil && lock.ExpiresAt.IsZero() {
		return nil, fmt.Errorf("factory_lock block in %s has no parseable expires_at", statePath)
	}
	return lock, nil
}

func mtime(path string) (time.Time, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// newestEntry returns the most recently modified file directly under
// dir.
func newestEntry(dir string) (string, time.Time, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}, false
	}
	var bestPath string
	var bestTime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestTime) {
			bestTime = info.ModTime()
			bestPath = filepath.Join(dir, e.Name())
		}
	}
	return bestPath, bestTime, bestPath != ""
}
