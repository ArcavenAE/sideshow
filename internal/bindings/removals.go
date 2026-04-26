package bindings

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SideshowSentinel is the marker that identifies a sideshow-managed
// binding output. ProcessRemovals will only delete entries that contain
// this sentinel — user-authored content with the same name is left
// alone.
const SideshowSentinel = "<!-- sideshow:fallback-resolution:begin -->"

// ProcessRemovals reads <packPath>/removals.txt (if present) and removes
// matching binding entries from user-scope target dirs. Each non-blank,
// non-comment line names a canonicalId — a skill directory under
// ~/.claude/skills/<id>/ or a command file at ~/.claude/commands/<id>.md.
// Only entries that carry the sideshow sentinel are removed; user-authored
// content with the same name is left alone.
//
// As a defensive measure, an entry is skipped (NOT removed) if the
// current pack still ships a binding by that canonicalId — the pack
// is the authority on what it currently provides.
//
// Returns the number of entries removed and the first error
// encountered. Per-entry errors are non-fatal; processing continues.
func ProcessRemovals(packPath string) (int, error) {
	removalsFile := filepath.Join(packPath, "removals.txt")
	f, err := os.Open(removalsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("open removals.txt: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Build the pack's current canonical set so we never delete what's
	// still shipped (defensive: upstream may carry stale entries).
	currentSkills := skillCanonicalIds(packPath)
	currentCommands := commandBasenames(packPath)

	removed := 0
	var firstErr error

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		canonicalID := line

		if _, stillShipped := currentSkills[canonicalID]; !stillShipped {
			ok, err := removeManagedSkill(canonicalID)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if ok {
				removed++
			}
		}

		cmdName := canonicalID + ".md"
		if _, stillShipped := currentCommands[cmdName]; !stillShipped {
			ok, err := removeManagedCommand(cmdName)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if ok {
				removed++
			}
		}
	}
	if err := scanner.Err(); err != nil && firstErr == nil {
		firstErr = err
	}

	return removed, firstErr
}

// removeManagedSkill deletes ~/.claude/skills/<canonicalId>/ if its
// SKILL.md carries the sideshow sentinel. Returns (true, nil) on
// successful removal, (false, nil) if not sideshow-managed or absent,
// (false, err) on filesystem error.
func removeManagedSkill(canonicalID string) (bool, error) {
	skillDir := filepath.Join(claudeSkillsDir(), canonicalID)
	skillEntry := filepath.Join(skillDir, "SKILL.md")
	owned, err := hasSentinel(skillEntry)
	if err != nil {
		return false, err
	}
	if !owned {
		return false, nil
	}
	if err := os.RemoveAll(skillDir); err != nil {
		return false, fmt.Errorf("remove skill %s: %w", canonicalID, err)
	}
	return true, nil
}

// removeManagedCommand deletes ~/.claude/commands/<name> if it carries
// the sideshow sentinel.
func removeManagedCommand(name string) (bool, error) {
	cmdFile := filepath.Join(claudeCommandsDir(), name)
	owned, err := hasSentinel(cmdFile)
	if err != nil {
		return false, err
	}
	if !owned {
		return false, nil
	}
	if err := os.Remove(cmdFile); err != nil {
		return false, fmt.Errorf("remove command %s: %w", name, err)
	}
	return true, nil
}

// hasSentinel reports whether path exists and contains the sideshow
// fallback-resolution sentinel. A non-existent file returns (false, nil)
// — that's a normal "not sideshow-managed" answer, not an error.
func hasSentinel(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return strings.Contains(string(data), SideshowSentinel), nil
}
