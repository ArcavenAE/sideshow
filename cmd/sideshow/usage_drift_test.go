package main

import (
	"regexp"
	"strings"
	"testing"
)

// Usage drift (sideshow#195). Each verb names its flags in a per-command
// usage string. Two checks hold the rest of the CLI to that text:
//
//  1. every flag a per-command usage names appears in the matching line of
//     `sideshow --help`, so a user reading help can find it;
//  2. the command's parser accepts every flag its usage names, so the
//     usage does not promise a flag the parser rejects.
//
// Known limit: a flag the parser accepts but no usage string names stays
// unpinned by both checks. Only a flag registry that the parsers, the
// usage strings and the help lines all read from would close that; a
// registry is not part of this change.
//
// coexist has a usage string too (cmd/sideshow/coexist.go) but is outside
// the scope this pin was approved for.

var (
	flagRE = regexp.MustCompile(`--[a-z][a-z-]*(?:\s+(?:<[^>\s]+>|[a-z]+(?:\|[a-z]+)+))?`)
	// An entry is a "  sideshow ..." line plus the indented lines that
	// continue it, since long entries wrap in usage().
	topLevelLine = regexp.MustCompile(`(?m)^  sideshow .*(?:\n {10,}\S.*)*`)
)

type usageFlag struct {
	name  string
	value string // "" for a boolean flag, else the usage's value token
}

// usageFlags returns the flags a usage string names, in order, each with
// the value token that follows it ("<path>", "local|project") if any.
func usageFlags(usage string) []usageFlag {
	var out []usageFlag
	for _, m := range flagRE.FindAllString(usage, -1) {
		name, value, _ := strings.Cut(m, " ")
		out = append(out, usageFlag{name: name, value: strings.TrimSpace(value)})
	}
	return out
}

// dummyValue returns an argument a parser will take for the value token.
func dummyValue(t *testing.T, token string) string {
	t.Helper()
	switch {
	case token == "":
		return ""
	case token == "<path>" || token == "<dir>":
		return t.TempDir()
	case strings.Contains(token, "|"):
		return strings.Split(token, "|")[0]
	default:
		return "dummy"
	}
}

// topLevelHelp returns the entries of `sideshow --help` that start a verb.
func topLevelHelp(t *testing.T) []string {
	t.Helper()
	bin := buildBinary(t)
	_, se, code := runBinary(t, bin, "--help")
	if code != 0 {
		t.Fatalf("--help: code=%d", code)
	}
	return topLevelLine.FindAllString(se, -1)
}

// lineFor returns the one top-level line starting with prefix that also
// satisfies keep; it fails the test if there is not exactly one.
func lineFor(t *testing.T, lines []string, prefix string, keep func(string) bool) string {
	t.Helper()
	var found []string
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) && keep(l) {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one top-level line starting %q, got %d: %q", prefix, len(found), found)
	}
	return found[0]
}

func anyLine(string) bool { return true }

// adoptModes splits adoptUsage by mode: the flags outside the
// --migrate-user-scope group and other than --finish belong to the
// conversion; the group's flags belong to migrate, which also honors
// --override-stale-lock (cmd/sideshow/adopt.go).
func adoptModes(t *testing.T) (conversion, migrate, finish []usageFlag) {
	t.Helper()
	before, group, ok := strings.Cut(adoptUsage, "[--migrate-user-scope")
	if !ok {
		t.Fatalf("adoptUsage has no --migrate-user-scope group: %s", adoptUsage)
	}
	for _, f := range usageFlags(before) {
		if f.name == "--finish" {
			finish = append(finish, f)
			continue
		}
		conversion = append(conversion, f)
		if f.name == "--override-stale-lock" {
			migrate = append(migrate, f)
		}
	}
	migrate = append(migrate, usageFlag{name: "--migrate-user-scope"})
	migrate = append(migrate, usageFlags(group)...)
	return conversion, migrate, finish
}

func TestUsageDrift_TopLevelHelpNamesEveryFlagOfEachUsageString(t *testing.T) {
	lines := topLevelHelp(t)
	conversion, migrate, finish := adoptModes(t)

	cases := []struct {
		name  string
		line  string
		flags []usageFlag
	}{
		{"enable", lineFor(t, lines, "  sideshow enable ", anyLine), usageFlags(enableUsage)},
		{"disable", lineFor(t, lines, "  sideshow disable ", anyLine), usageFlags(disableUsage)},
		{"activate", lineFor(t, lines, "  sideshow activate ", anyLine), usageFlags(activateUsage)},
		{"deactivate", lineFor(t, lines, "  sideshow deactivate ", anyLine), usageFlags(deactivateUsage)},
		{"coexist-check", lineFor(t, lines, "  sideshow coexist-check ", anyLine), usageFlags(coexistCheckUsage)},
		{"adopt conversion", lineFor(t, lines, "  sideshow adopt ", func(l string) bool {
			return !strings.Contains(l, "--finish") && !strings.Contains(l, "--migrate-user-scope")
		}), conversion},
		{"adopt migrate", lineFor(t, lines, "  sideshow adopt ", func(l string) bool { return strings.Contains(l, "--migrate-user-scope") }), migrate},
		{"adopt finish", lineFor(t, lines, "  sideshow adopt ", func(l string) bool { return strings.Contains(l, "--finish") }), finish},
	}
	for _, c := range cases {
		if len(c.flags) == 0 {
			t.Errorf("%s: no flags read from its usage string; the pin would pass on nothing", c.name)
		}
		for _, f := range c.flags {
			if !strings.Contains(c.line, f.name) {
				t.Errorf("%s: top-level help line omits %s: %q", c.name, f.name, c.line)
			}
		}
	}
	// A conversion takes <pack>[@<version>]; the help line must show it.
	conv := lineFor(t, lines, "  sideshow adopt ", func(l string) bool {
		return !strings.Contains(l, "--finish") && !strings.Contains(l, "--migrate-user-scope")
	})
	if !strings.Contains(conv, "[@<ver") {
		t.Errorf("adopt conversion help line omits the [@<version>] form its usage names: %q", conv)
	}
}

// isolate points every directory the commands read at scratch space and
// runs the test from an empty working directory, so a flag run through a
// full command cannot touch the developer's home or this checkout.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SIDESHOW_HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Chdir(t.TempDir())
}

func TestUsageDrift_EveryParserAcceptsEveryFlagItsUsageNames(t *testing.T) {
	conversion, migrate, finish := adoptModes(t)

	runArgs := func(t *testing.T, run func([]string) error, f usageFlag) error {
		t.Helper()
		args := []string{"demo", f.name}
		if f.value != "" {
			args = append(args, dummyValue(t, f.value))
		}
		_, err := captureStdout(t, func() error { return run(args) })
		return err
	}
	parseVerb := func(verb string) func([]string) error {
		return func(args []string) error { _, err := parseVerbArgs(verb, args); return err }
	}
	parseActivate := func(verb string, allowAgent bool) func([]string) error {
		return func(args []string) error { _, _, err := parseActivateArgs(verb, args, allowAgent); return err }
	}

	cases := []struct {
		name  string
		flags []usageFlag
		run   func([]string) error
	}{
		{"enable", usageFlags(enableUsage), parseVerb("enable")},
		{"disable", usageFlags(disableUsage), parseVerb("disable")},
		{"activate", usageFlags(activateUsage), parseActivate("activate", true)},
		{"deactivate", usageFlags(deactivateUsage), parseActivate("deactivate", false)},
		{"coexist-check", usageFlags(coexistCheckUsage), runCoexistCheck},
		{"adopt conversion", conversion, runAdopt},
		{"adopt migrate", migrate, runAdopt},
		{"adopt finish", finish, runAdopt},
	}
	for _, c := range cases {
		for _, f := range c.flags {
			t.Run(c.name+"/"+f.name, func(t *testing.T) {
				isolate(t)
				err := runArgs(t, c.run, f)
				if err != nil && strings.Contains(err.Error(), "unknown flag") {
					t.Errorf("%s rejects %s, which its usage names: %v", c.name, f.name, err)
				}
			})
		}
	}
}
