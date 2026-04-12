package distribute

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArcavenAE/sideshow/internal/pack"
	"github.com/ArcavenAE/sideshow/internal/project"
)

// markerPrefix is used to identify sideshow-managed files and sections.
const markerPrefix = "<!-- managed by sideshow"

// sectionBegin returns the begin marker for a CLAUDE.md section.
func sectionBegin(id string) string {
	return fmt.Sprintf("<!-- sideshow:%s:begin -->", id)
}

// sectionEnd returns the end marker for a CLAUDE.md section.
func sectionEnd(id string) string {
	return fmt.Sprintf("<!-- sideshow:%s:end -->", id)
}

// fileMarker returns the ownership marker for a rules file.
func fileMarker(packName, version string) string {
	return fmt.Sprintf("<!-- managed by sideshow:%s:%s -->", packName, version)
}

// hookManagedBy returns the _managed_by value for a settings.json hook.
func hookManagedBy(packName string) string {
	return fmt.Sprintf("sideshow:%s", packName)
}

// Result tracks the outcome of distributing to one repo.
type Result struct {
	RepoName string
	Actions  []Action
	Skipped  bool  // true if directory doesn't exist
	Error    error // fatal error for this repo
}

// Action is one artifact distribution action.
type Action struct {
	Type     string // "rules", "hook", "claude_md", "symlink", "gitignore"
	Path     string // file path affected
	Status   string // "wrote", "merged", "skipped", "conflict", "error"
	Detail   string // human-readable explanation
	Artifact pack.DistributedArtifact
}

// Options controls distribution behavior.
type Options struct {
	DryRun      bool
	PackName    string
	PackVersion string
	PackRoot    string // resolved pack root on disk
}

// ToRepo distributes artifacts from the manifest to a single subrepo.
func ToRepo(repo project.Subrepo, manifest *Manifest, opts Options) Result {
	result := Result{RepoName: repo.Name}

	if !repo.Present {
		result.Skipped = true
		return result
	}

	for _, rule := range manifest.Rules {
		action := distributeRule(repo.AbsPath, rule, opts)
		result.Actions = append(result.Actions, action)
	}

	for _, hook := range manifest.Hooks {
		action := distributeHook(repo.AbsPath, hook, opts)
		result.Actions = append(result.Actions, action)
	}

	for _, section := range manifest.ClaudeMD {
		action := distributeClaudeMD(repo.AbsPath, section, opts)
		result.Actions = append(result.Actions, action)
	}

	for _, link := range manifest.Symlinks {
		action := distributeSymlink(repo.AbsPath, link, opts)
		result.Actions = append(result.Actions, action)
	}

	for _, line := range manifest.Gitignore {
		action := distributeGitignore(repo.AbsPath, line, opts)
		result.Actions = append(result.Actions, action)
	}

	return result
}

// RecordResults writes distribution results to the registry ledger.
func RecordResults(reg *pack.Registry, projectID, root, manifest string, results []Result, opts Options) {
	proj := reg.FindOrCreateProject(projectID)
	inst := proj.FindOrCreateInstallation(root, manifest)
	inst.LastSeen = time.Now().UTC().Format(time.RFC3339)

	for _, res := range results {
		if res.Skipped || res.Error != nil {
			continue
		}

		// Find or create repo entry
		var repoDistrib *pack.RepoDistribution
		for i := range inst.Repos {
			if inst.Repos[i].Name == res.RepoName {
				repoDistrib = &inst.Repos[i]
				break
			}
		}
		if repoDistrib == nil {
			inst.Repos = append(inst.Repos, pack.RepoDistribution{Name: res.RepoName})
			repoDistrib = &inst.Repos[len(inst.Repos)-1]
		}

		// Find subrepo path from results
		for _, r := range results {
			if r.RepoName == res.RepoName && !r.Skipped {
				// Path is set from the subrepo data
				break
			}
		}

		// Collect artifacts that were actually written
		var artifacts []pack.DistributedArtifact
		for _, action := range res.Actions {
			if action.Status == "wrote" || action.Status == "merged" {
				artifacts = append(artifacts, action.Artifact)
			}
		}

		if len(artifacts) == 0 {
			continue
		}

		// Update or create pack distribution entry
		var packDistrib *pack.PackDistribution
		for i := range repoDistrib.Packs {
			if repoDistrib.Packs[i].Pack == opts.PackName {
				packDistrib = &repoDistrib.Packs[i]
				break
			}
		}
		if packDistrib == nil {
			repoDistrib.Packs = append(repoDistrib.Packs, pack.PackDistribution{
				Pack:  opts.PackName,
				Scope: "project",
			})
			packDistrib = &repoDistrib.Packs[len(repoDistrib.Packs)-1]
		}

		packDistrib.Version = opts.PackVersion
		packDistrib.DistributedAt = time.Now().UTC().Format(time.RFC3339)
		packDistrib.Artifacts = artifacts
	}
}

// --- Per-artifact-type distribution logic ---

// distributeRule copies a rules file with ownership marker.
// Safety: skip if exists without marker (user-authored).
func distributeRule(repoRoot string, rule RuleArtifact, opts Options) Action {
	targetPath := filepath.Join(repoRoot, rule.Target)
	action := Action{
		Type: "rules",
		Path: rule.Target,
	}

	// Read source content
	sourcePath := filepath.Join(opts.PackRoot, rule.Source)
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("read source: %v", err)
		return action
	}

	// Prepend ownership marker
	marker := fileMarker(opts.PackName, opts.PackVersion)
	content := marker + "\n" + string(sourceData)

	// Check if target exists
	existingData, err := os.ReadFile(targetPath)
	if err == nil {
		// File exists — check if sideshow owns it
		if !strings.HasPrefix(string(existingData), markerPrefix) {
			action.Status = "skipped"
			action.Detail = "exists without sideshow marker (user-authored)"
			return action
		}
		// Sideshow owns it — overwrite
	}

	if opts.DryRun {
		if err == nil && strings.HasPrefix(string(existingData), markerPrefix) {
			action.Status = "wrote"
			action.Detail = "would update (sideshow-managed)"
		} else if err == nil {
			action.Status = "skipped"
			action.Detail = "exists without sideshow marker (user-authored)"
		} else {
			action.Status = "wrote"
			action.Detail = "would create"
		}
		return action
	}

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("create dir: %v", err)
		return action
	}

	if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("write: %v", err)
		return action
	}

	action.Status = "wrote"
	action.Detail = "created"
	if existingData != nil {
		action.Detail = "updated (sideshow-managed)"
	}
	action.Artifact = pack.DistributedArtifact{
		Type:     "rules",
		Path:     rule.Target,
		Checksum: "sha256:" + sha256hex([]byte(content)),
	}
	return action
}

// distributeHook merges a hook into .claude/settings.json.
// Safety: parse JSON, merge into event array, skip duplicates.
//
// Claude Code's hook format is:
//
//	{
//	  "hooks": {
//	    "EventName": [
//	      {
//	        "matcher": "",
//	        "hooks": [
//	          {"type": "command", "command": "the command"}
//	        ]
//	      }
//	    ]
//	  }
//	}
//
// Each event has an array of rule groups. Each group has a matcher
// (pattern, empty = match all) and a hooks array. sideshow adds
// a new rule group with empty matcher and a _managed_by field.
func distributeHook(repoRoot string, hook HookArtifact, opts Options) Action {
	settingsPath := filepath.Join(repoRoot, ".claude", "settings.json")
	action := Action{
		Type: "hook",
		Path: ".claude/settings.json",
	}

	// Load existing settings
	existingData, readErr := os.ReadFile(settingsPath)
	var settings map[string]any

	if readErr == nil {
		if err := json.Unmarshal(existingData, &settings); err != nil {
			action.Status = "error"
			action.Detail = fmt.Sprintf("parse settings.json: %v (not touching it)", err)
			return action
		}
	} else if os.IsNotExist(readErr) {
		settings = make(map[string]any)
	} else {
		action.Status = "error"
		action.Detail = fmt.Sprintf("read settings.json: %v", readErr)
		return action
	}

	// Navigate to hooks section
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = make(map[string]any)
		settings["hooks"] = hooks
	}

	eventList, _ := hooks[hook.Event].([]any)

	// Check for duplicate: scan all rule groups' hooks arrays for same command
	if hookExistsInEvent(eventList, hook.Command) {
		action.Status = "skipped"
		action.Detail = fmt.Sprintf("%s hook already exists", hook.Event)
		return action
	}

	if opts.DryRun {
		action.Status = "merged"
		action.Detail = fmt.Sprintf("would add %s hook: %s", hook.Event, hook.Command)
		return action
	}

	// Add a new rule group in Claude Code's format
	newRuleGroup := map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": hook.Command,
			},
		},
		"_managed_by": hookManagedBy(opts.PackName),
	}
	eventList = append(eventList, newRuleGroup)
	hooks[hook.Event] = eventList

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("create .claude dir: %v", err)
		return action
	}

	// Write back
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("marshal settings: %v", err)
		return action
	}

	if err := os.WriteFile(settingsPath, append(data, '\n'), 0o644); err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("write settings: %v", err)
		return action
	}

	action.Status = "merged"
	action.Detail = fmt.Sprintf("added %s hook: %s", hook.Event, hook.Command)
	action.Artifact = pack.DistributedArtifact{
		Type:    "hook",
		Event:   hook.Event,
		Command: hook.Command,
	}
	return action
}

// hookExistsInEvent checks whether a command already exists in an event's
// rule groups. Handles both the nested Claude Code format and flat format.
func hookExistsInEvent(eventList []any, command string) bool {
	for _, entry := range eventList {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		// Nested format: rule group with "hooks" array
		if innerHooks, ok := m["hooks"].([]any); ok {
			for _, h := range innerHooks {
				if hm, ok := h.(map[string]any); ok {
					if cmd, _ := hm["command"].(string); cmd == command {
						return true
					}
				}
			}
		}

		// Flat format (legacy/simple): direct "command" key
		if cmd, _ := m["command"].(string); cmd == command {
			return true
		}
	}
	return false
}

// distributeClaudeMD injects a section into CLAUDE.md using markers.
// Safety: only modifies content between markers, appends if no markers.
func distributeClaudeMD(repoRoot string, section ClaudeMDArtifact, opts Options) Action {
	claudeMDPath := filepath.Join(repoRoot, "CLAUDE.md")
	action := Action{
		Type: "claude_md",
		Path: "CLAUDE.md",
	}

	// Read source content
	sourcePath := filepath.Join(opts.PackRoot, section.Source)
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("read source: %v", err)
		return action
	}

	begin := sectionBegin(section.ID)
	end := sectionEnd(section.ID)
	injected := begin + "\n" + string(sourceData) + "\n" + end

	// Read existing CLAUDE.md
	existingData, readErr := os.ReadFile(claudeMDPath)

	if readErr != nil && !os.IsNotExist(readErr) {
		action.Status = "error"
		action.Detail = fmt.Sprintf("read CLAUDE.md: %v", readErr)
		return action
	}

	var newContent string

	if readErr == nil {
		existing := string(existingData)

		// Check for existing markers
		beginIdx := strings.Index(existing, begin)
		endIdx := strings.Index(existing, end)

		if beginIdx >= 0 && endIdx > beginIdx {
			// Replace between markers
			newContent = existing[:beginIdx] + injected + existing[endIdx+len(end):]
			if opts.DryRun {
				action.Status = "wrote"
				action.Detail = fmt.Sprintf("would update section %s (markers exist)", section.ID)
				return action
			}
		} else {
			// Append
			newContent = strings.TrimRight(existing, "\n") + "\n\n" + injected + "\n"
			if opts.DryRun {
				action.Status = "wrote"
				action.Detail = fmt.Sprintf("would append section %s", section.ID)
				return action
			}
		}
	} else {
		// Create new file
		newContent = injected + "\n"
		if opts.DryRun {
			action.Status = "wrote"
			action.Detail = fmt.Sprintf("would create CLAUDE.md with section %s", section.ID)
			return action
		}
	}

	if err := os.WriteFile(claudeMDPath, []byte(newContent), 0o644); err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("write CLAUDE.md: %v", err)
		return action
	}

	action.Status = "wrote"
	action.Detail = fmt.Sprintf("injected section %s", section.ID)
	action.Artifact = pack.DistributedArtifact{
		Type:      "claude_md",
		SectionID: section.ID,
	}
	return action
}

// distributeSymlink creates a symlink in the repo.
// Safety: skip if exists and correct, report if wrong target or regular file.
func distributeSymlink(repoRoot string, link SymlinkArtifact, opts Options) Action {
	linkPath := filepath.Join(repoRoot, link.Path)
	action := Action{
		Type: "symlink",
		Path: link.Path,
	}

	// Check existing state
	info, lstatErr := os.Lstat(linkPath)

	if lstatErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			// It's a symlink — check target
			target, err := os.Readlink(linkPath)
			if err == nil && target == link.Target {
				action.Status = "skipped"
				action.Detail = "symlink already correct"
				return action
			}
			action.Status = "conflict"
			action.Detail = fmt.Sprintf("symlink exists but points to %q (expected %q)", target, link.Target)
			return action
		}
		// Regular file at that path
		action.Status = "conflict"
		action.Detail = "regular file exists at symlink path"
		return action
	}

	if opts.DryRun {
		action.Status = "wrote"
		action.Detail = fmt.Sprintf("would create symlink %s → %s", link.Path, link.Target)
		return action
	}

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("create dir: %v", err)
		return action
	}

	if err := os.Symlink(link.Target, linkPath); err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("create symlink: %v", err)
		return action
	}

	action.Status = "wrote"
	action.Detail = fmt.Sprintf("created %s → %s", link.Path, link.Target)
	action.Artifact = pack.DistributedArtifact{
		Type:   "symlink",
		Path:   link.Path,
		Target: link.Target,
	}
	return action
}

// distributeGitignore appends a line to .gitignore if missing.
// Safety: never removes or reorders lines.
func distributeGitignore(repoRoot string, line string, opts Options) Action {
	giPath := filepath.Join(repoRoot, ".gitignore")
	action := Action{
		Type: "gitignore",
		Path: ".gitignore",
	}

	existing, err := os.ReadFile(giPath)
	if err != nil && !os.IsNotExist(err) {
		action.Status = "error"
		action.Detail = fmt.Sprintf("read .gitignore: %v", err)
		return action
	}

	// Check if line already present
	if err == nil {
		for _, existingLine := range strings.Split(string(existing), "\n") {
			if strings.TrimSpace(existingLine) == strings.TrimSpace(line) {
				action.Status = "skipped"
				action.Detail = "line already present"
				return action
			}
		}
	}

	if opts.DryRun {
		action.Status = "wrote"
		action.Detail = fmt.Sprintf("would append %q", line)
		return action
	}

	// Append the line
	content := string(existing)
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += line + "\n"

	if err := os.WriteFile(giPath, []byte(content), 0o644); err != nil {
		action.Status = "error"
		action.Detail = fmt.Sprintf("write .gitignore: %v", err)
		return action
	}

	action.Status = "wrote"
	action.Detail = fmt.Sprintf("appended %q", line)
	action.Artifact = pack.DistributedArtifact{
		Type: "gitignore",
		Line: line,
	}
	return action
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
