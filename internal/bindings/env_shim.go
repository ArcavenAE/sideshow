package bindings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The runtime env shim is route (a) of the CLAUDE_PLUGIN_ROOT
// resolution decision (aae-orc-d3nq.50): keep the variable NAME, set
// its VALUE in the repo settings file at enable time. Trial-verified
// (finding-091 addendum 2, T15): a settings env entry reaches hook
// execution, main-loop Bash, and Bash inside subagents, resolving
// every store-relative reference in the pack's content with zero
// rewriting — byte identity preserved for the equivalence proof.
//
// These are read-merge-write primitives on one settings JSON file;
// the enable/disable verbs (.7) choose which file (settings.local.json
// at local scope, settings.json at project scope) and record the
// artifact in the ledger.

// MergeEnvShim sets env[name] = value in the settings file at path,
// creating the file when absent and preserving every other key. It
// reports whether the file itself was created (the caller records
// that for exact removal). A pre-existing env[name] with a different
// value refuses with ErrWouldClobber — sideshow never overwrites
// repo content it does not own; same-value entries are idempotent.
func MergeEnvShim(path, name, value string) (created bool, err error) {
	settings, existed, err := readSettings(path)
	if err != nil {
		return false, err
	}

	env, err := envObject(settings, path)
	if err != nil {
		return false, err
	}
	if prev, ok := env[name]; ok {
		if s, isStr := prev.(string); isStr && s == value {
			return !existed, writeSettings(path, settings)
		}
		return false, fmt.Errorf("%w: %s already sets env.%s=%v", ErrWouldClobber, path, name, prev)
	}
	env[name] = value
	settings["env"] = env

	return !existed, writeSettings(path, settings)
}

// RemoveEnvShim removes env[name] from the settings file only when it
// still holds value — removal never guesses; a drifted or foreign
// entry is left in place and reported via removed=false. An empty env
// object is pruned. Whether to delete a file MergeEnvShim created is
// the caller's call, made from its artifact record.
func RemoveEnvShim(path, name, value string) (removed bool, err error) {
	settings, existed, err := readSettings(path)
	if err != nil {
		return false, err
	}
	if !existed {
		return false, nil
	}
	env, err := envObject(settings, path)
	if err != nil {
		return false, err
	}
	s, isStr := env[name].(string)
	if !isStr || s != value {
		return false, nil
	}
	delete(env, name)
	if len(env) == 0 {
		delete(settings, "env")
	} else {
		settings["env"] = env
	}
	return true, writeSettings(path, settings)
}

// VerifyEnvShim checks that the settings file resolves name to
// wantValue — the bind-time check behind "zero unresolved references
// in materialized output", re-run by doctor. The error text names the
// drift so callers can print it verbatim.
func VerifyEnvShim(path, name, wantValue string) error {
	settings, existed, err := readSettings(path)
	if err != nil {
		return err
	}
	if !existed {
		return fmt.Errorf("env shim missing: %s does not exist, so %s is unresolved in this repo", path, name)
	}
	env, err := envObject(settings, path)
	if err != nil {
		return err
	}
	got, ok := env[name].(string)
	switch {
	case !ok:
		return fmt.Errorf("env shim missing: %s has no env.%s entry", path, name)
	case got != wantValue:
		return fmt.Errorf("env shim drifted: %s sets env.%s=%q, want %q", path, name, got, wantValue)
	}
	return nil
}

// InlineEnvBelt returns the NAME='value' prefix every synthesized
// hook command carries (the belt to the settings-env suspender): the
// hook surface keeps working even where the settings env leg is
// absent or drifted. The value is single-quoted with POSIX-safe
// escaping.
func InlineEnvBelt(name, value string) string {
	quoted := "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
	return name + "=" + quoted + " "
}

// readSettings loads a settings JSON object. A missing file returns
// an empty object with existed=false; malformed JSON is an error
// (fail closed — merging into a file we cannot parse risks destroying
// user configuration).
func readSettings(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, false, nil
		}
		return nil, false, fmt.Errorf("read settings %s: %w", path, err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, false, fmt.Errorf("parse settings %s (refusing to merge into a file that cannot round-trip): %w", path, err)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, true, nil
}

// envObject returns the settings' env block as a mutable map, failing
// closed when env exists but is not an object.
func envObject(settings map[string]any, path string) (map[string]any, error) {
	raw, ok := settings["env"]
	if !ok {
		return map[string]any{}, nil
	}
	env, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("settings %s: env is not an object; refusing to merge", path)
	}
	return env, nil
}

// writeSettings persists a settings object with stable two-space
// indentation and a trailing newline.
func writeSettings(path string, settings map[string]any) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write settings %s: %w", path, err)
	}
	return nil
}
