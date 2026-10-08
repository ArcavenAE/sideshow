package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/enable"
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
// registry is not part of this change. The project init parser has a
// second gap: it ignores flags it does not know, so check 2 cannot see a
// flag dropped from it.

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
		{"coexist", lineFor(t, lines, "  sideshow coexist ", anyLine), usageFlags(coexistUsage)},
		{"project init", lineFor(t, lines, "  sideshow project init ", anyLine), usageFlags(projectInitUsage)},
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

// How a command's parser is reached. A parse-only verb has a parser that
// does no work, so any error at all means it rejected the flag. The others
// run the whole command, whose later steps fail for unrelated reasons in
// scratch space, so only an error that names the flag counts.
func TestUsageDrift_EveryParserAcceptsEveryFlagItsUsageNames(t *testing.T) {
	conversion, migrate, finish := adoptModes(t)

	parseOnly := func(parse func([]string) error) func([]string) (bool, error) {
		return func(args []string) (bool, error) { return true, parse(args) }
	}
	fullRun := func(run func([]string) error) func([]string) (bool, error) {
		return func(args []string) (bool, error) { return false, run(args) }
	}
	parseVerb := func(parse func([]string) (*enable.Options, error)) func([]string) error {
		return func(args []string) error { _, err := parse(args); return err }
	}
	parseActivateVerb := func(parse func([]string) (*enable.Options, string, error)) func([]string) error {
		return func(args []string) error { _, _, err := parse(args); return err }
	}

	cases := []struct {
		name   string
		flags  []usageFlag
		prefix func(usageFlag) []string // arguments the mode needs before the flag
		run    func([]string) (parseOnly bool, err error)
	}{
		{"enable", usageFlags(enableUsage), nil, parseOnly(parseVerb(parseEnableArgs))},
		{"disable", usageFlags(disableUsage), nil, parseOnly(parseVerb(parseDisableArgs))},
		{"activate", usageFlags(activateUsage), nil, parseOnly(parseActivateVerb(parseActivate))},
		{"deactivate", usageFlags(deactivateUsage), nil, parseOnly(parseActivateVerb(parseDeactivate))},
		{"coexist-check", usageFlags(coexistCheckUsage), nil, fullRun(runCoexistCheck)},
		{"coexist", usageFlags(coexistUsage), nil, fullRun(runCoexist)},
		{"project init", usageFlags(projectInitUsage), nil, fullRun(runProjectInitForPack)},
		{"adopt conversion", conversion, nil, fullRun(runAdopt)},
		{"adopt migrate", migrate, func(f usageFlag) []string {
			// A migrate flag is meaningful, and not an error in its own
			// right, only beside the mode flag (--yes alone is refused).
			if f.name == "--migrate-user-scope" {
				return nil
			}
			return []string{"--migrate-user-scope"}
		}, fullRun(runAdopt)},
		{"adopt finish", finish, nil, fullRun(runAdopt)},
	}
	for _, c := range cases {
		for _, f := range c.flags {
			t.Run(c.name+"/"+f.name, func(t *testing.T) {
				isolate(t)
				args := []string{"demo"}
				if c.prefix != nil {
					args = append(args, c.prefix(f)...)
				}
				args = append(args, f.name)
				if f.value != "" {
					args = append(args, dummyValue(t, f.value))
				}
				var (
					only bool
					err  error
				)
				if _, cerr := captureStdout(t, func() error { only, err = c.run(args); return nil }); cerr != nil {
					t.Fatal(cerr)
				}
				switch {
				case only && err != nil:
					t.Errorf("%s's parser rejects %s, which its usage names: %v", c.name, f.name, err)
				case !only && err != nil && strings.Contains(err.Error(), f.name):
					t.Errorf("%s fails on %s, which its usage names: %v", c.name, f.name, err)
				}
			})
		}
	}
}
