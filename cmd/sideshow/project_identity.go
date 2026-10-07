package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// seedsIdentity names the packs whose runtime reads core.user_name and
// core.project_name from the human-authored user layer. The pack ships
// neither (the build scrubs the CI identity), so a bound repo resolves
// both as absent until init supplies them (aae-orc-jy1j, F-c).
func seedsIdentity(packName string) bool { return packName == "bmad" }

// seedIdentity writes core.user_name and core.project_name into
// <customDir>/config.user.toml, which resolves as the last layer of the
// pack's config chain. It adds only keys that are absent, never rewrites
// a value, and never invents a name: user_name comes from the flag, else
// git config user.name, else it stays absent. The layer holds a person's
// name, so nothing is written unless git ignores the file.
func seedIdentity(repoDir, customDir, flagName string, dryRun bool) error {
	layer := filepath.Join(customDir, "config.user.toml")
	rel, err := filepath.Rel(repoDir, layer)
	if err != nil {
		return fmt.Errorf("locate identity layer: %w", err)
	}
	rel = filepath.ToSlash(rel)

	userName := strings.TrimSpace(flagName)
	if userName == "" {
		userName = gitConfigUserName(repoDir)
	}
	want := []identityKey{{"project_name", filepath.Base(repoDir)}}
	if userName != "" {
		want = append([]identityKey{{"user_name", userName}}, want...)
	}

	// A dry run has not applied the distribute step that gitignores the
	// layer, so the check would misreport what the real run does.
	if !dryRun && !gitIgnores(repoDir, rel) {
		fmt.Printf("  identity: skipped, %s is not gitignored here, and it would hold a person's name\n", rel)
		return nil
	}

	data, err := os.ReadFile(layer)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", rel, err)
	}
	if inlineCore.Match(data) {
		fmt.Printf("  identity: skipped, %s defines core as an inline table; add user_name and project_name to it by hand\n", rel)
		return nil
	}
	// One line, once, when no name resolves and the file has none.
	if userName == "" {
		if _, wouldAdd := addCoreKeys(string(data), []identityKey{{"user_name", ""}}); len(wouldAdd) > 0 {
			defer fmt.Println("user_name not set: pass --user-name or set git user.name")
		}
	}
	updated, added := addCoreKeys(string(data), want)
	if len(added) == 0 {
		fmt.Printf("  identity: %s already has %s\n", rel, strings.Join(keyNames(want), " and "))
		return nil
	}
	if dryRun {
		fmt.Printf("  would seed %s in %s\n", strings.Join(added, " and "), rel)
		return nil
	}
	if err := os.WriteFile(layer, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	fmt.Printf("  identity: seeded %s in %s\n", strings.Join(added, " and "), rel)
	return nil
}

type identityKey struct{ name, value string }

func keyNames(keys []identityKey) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.name
	}
	return out
}

func gitConfigUserName(repoDir string) string {
	out, err := exec.Command("git", "-C", repoDir, "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitIgnores reports whether git ignores rel in repoDir. Any other
// outcome (not ignored, not a repository, git missing) is false, so the
// caller writes nothing it cannot show is kept out of the repo.
func gitIgnores(repoDir, rel string) bool {
	return exec.Command("git", "-C", repoDir, "check-ignore", "-q", "--", rel).Run() == nil
}

var (
	tableHeader = regexp.MustCompile(`^\s*\[([^\[\]]+)\]\s*(#.*)?$`)
	arrayHeader = regexp.MustCompile(`^\s*\[\[`)
	inlineCore  = regexp.MustCompile(`(?m)^\s*core\s*=`)
)

// addCoreKeys returns content with each wanted key set under [core]
// when the file does not already define it, and the names it added.
// A key counts as defined when it is assigned inside [core] or as a
// dotted core.<key> at the top level. The edit is line-based on
// purpose: the user's comments, ordering and other tables stay as they
// are.
func addCoreKeys(content string, want []identityKey) (string, []string) {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if content == "" {
		lines = nil
	}

	defined := map[string]bool{}
	coreHeader := -1
	table := ""
	for i, l := range lines {
		if arrayHeader.MatchString(l) {
			table = "[[array]]"
			continue
		}
		if m := tableHeader.FindStringSubmatch(l); m != nil {
			table = strings.TrimSpace(m[1])
			if table == "core" && coreHeader < 0 {
				coreHeader = i
			}
			continue
		}
		for _, k := range want {
			plain := regexp.MustCompile(`^\s*` + k.name + `\s*=`)
			dotted := regexp.MustCompile(`^\s*core\.` + k.name + `\s*=`)
			if (table == "core" && plain.MatchString(l)) || (table == "" && dotted.MatchString(l)) {
				defined[k.name] = true
			}
		}
	}

	var add []string
	var added []string
	for _, k := range want {
		if defined[k.name] {
			continue
		}
		add = append(add, k.name+" = "+tomlString(k.value))
		added = append(added, k.name)
	}
	if len(add) == 0 {
		return content, nil
	}

	if coreHeader >= 0 {
		out := append([]string{}, lines[:coreHeader+1]...)
		out = append(out, add...)
		out = append(out, lines[coreHeader+1:]...)
		return strings.Join(out, "\n") + "\n", added
	}
	out := append([]string{}, lines...)
	if len(out) > 0 {
		out = append(out, "")
	}
	out = append(out, "[core]")
	out = append(out, add...)
	return strings.Join(out, "\n") + "\n", added
}

// tomlString quotes s as a TOML basic string. JSON string escapes are a
// subset of TOML's, so the JSON encoder (without HTML escaping) is
// enough for names and directory names.
func tomlString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
