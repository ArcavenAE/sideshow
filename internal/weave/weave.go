// Package weave applies a repo's declarative post-install operations to a
// freshly installed pack tree. See docs/pack-weaving-spec.md for the contract.
//
// The installer owns the pack directory. The repo owns everything it adds to
// it. Weaving is how the repo's additions survive the installer.
package weave

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the weave.yaml schema version this build understands.
const SchemaVersion = "0.1.0"

// Outcome is the result of one operation. Three states, not two: a skipped
// operation is reported and counted rather than silent, because a weave that
// skipped everything should not look like a weave that applied everything.
type Outcome string

const (
	Applied Outcome = "applied"
	Skipped Outcome = "skipped"
	Failed  Outcome = "failed"
)

// Action is one operation's result.
type Action struct {
	Type    string // "csv", "memory", "shim", "patch", "verify"
	Name    string // declaration name or id
	Path    string // file affected, when there is exactly one
	Outcome Outcome
	Detail  string
}

// Result aggregates a whole weave run.
type Result struct {
	Actions []Action
}

// Failures returns the actions that failed.
func (r Result) Failures() []Action {
	var out []Action
	for _, a := range r.Actions {
		if a.Outcome == Failed {
			out = append(out, a)
		}
	}
	return out
}

// Counts returns applied, skipped, and failed totals.
func (r Result) Counts() (applied, skipped, failed int) {
	for _, a := range r.Actions {
		switch a.Outcome {
		case Applied:
			applied++
		case Skipped:
			skipped++
		case Failed:
			failed++
		}
	}
	return
}

// Options controls a weave run.
type Options struct {
	DryRun bool
	Strict bool // failures become an error rather than a warning
}

// Declaration is a parsed weave.yaml.
type Declaration struct {
	SchemaVersion string            `yaml:"schema_version"`
	Pack          string            `yaml:"pack"`
	Vars          map[string]string `yaml:"vars"`

	CustomAgents     []CustomAgent     `yaml:"custom_agents"`
	CSVInjections    []CSVInjection    `yaml:"csv_injections"`
	MemoryInjections []MemoryInjection `yaml:"memory_injections"`
	SlashCommands    []SlashCommand    `yaml:"slash_commands"`
	Verification     *Verification     `yaml:"verification"`

	// ApplyUpstreamPatches is `none` or a list. finding-029's table claimed the
	// first ported project applies patches; its script contains no patch code
	// at all (see finding-097), so this
	// operation has no verified prior art yet and stays a stub. See
	// ops_patch.go.
	ApplyUpstreamPatches yaml.Node `yaml:"apply_upstream_patches"`
}

// CustomAgent records an agent the repo adds. Declarative only today: nothing
// in the first port reads it beyond reporting.
type CustomAgent struct {
	ID             string `yaml:"id"`
	Name           string `yaml:"name"`
	Source         string `yaml:"source"`
	SlashCommandID string `yaml:"slash_command_id"`
}

// varPattern matches a {{name}} substitution site.
var varPattern = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`)

// Load reads and validates a weave declaration.
func Load(path string) (*Declaration, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// KnownFields makes an unrecognized key inside a recognized operation an
	// error. Silently ignoring a misspelled on_missing_anchor would mean
	// running the destructive default.
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	var d Declaration
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}

	if d.SchemaVersion == "" {
		return nil, fmt.Errorf("%s: schema_version is required", filepath.Base(path))
	}
	if err := checkSchemaVersion(d.SchemaVersion); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if d.Pack == "" {
		return nil, fmt.Errorf("%s: pack is required", filepath.Base(path))
	}

	if err := d.substituteVars(); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return &d, nil
}

// checkSchemaVersion accepts an exact minor match for 0.x.
func checkSchemaVersion(v string) error {
	want := strings.Split(SchemaVersion, ".")
	got := strings.Split(v, ".")
	if len(got) < 2 {
		return fmt.Errorf("schema_version %q is not major.minor.patch", v)
	}
	if got[0] != want[0] || got[1] != want[1] {
		return fmt.Errorf("schema_version %s is not supported by this sideshow "+
			"(understands %s)", v, SchemaVersion)
	}
	return nil
}

// expand substitutes {{name}} from vars. An unresolved var is an error, not an
// empty string: a silent empty substitution writes to a path one segment short
// of the intended one.
func (d *Declaration) expand(s string) (string, error) {
	var missing []string
	out := varPattern.ReplaceAllStringFunc(s, func(m string) string {
		key := varPattern.FindStringSubmatch(m)[1]
		if v, ok := d.Vars[key]; ok {
			return v
		}
		missing = append(missing, key)
		return m
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("unresolved var(s) %s in %q",
			strings.Join(missing, ", "), s)
	}
	return out, nil
}

// substituteVars expands every substitution site in the declaration.
func (d *Declaration) substituteVars() error {
	var err error
	exp := func(p *string) {
		if err != nil {
			return
		}
		var v string
		if v, err = d.expand(*p); err == nil {
			*p = v
		}
	}

	for i := range d.CustomAgents {
		exp(&d.CustomAgents[i].ID)
		exp(&d.CustomAgents[i].Source)
		exp(&d.CustomAgents[i].SlashCommandID)
	}
	for i := range d.CSVInjections {
		exp(&d.CSVInjections[i].Target)
		exp(&d.CSVInjections[i].Guard)
	}
	for i := range d.MemoryInjections {
		for j := range d.MemoryInjections[i].Targets {
			exp(&d.MemoryInjections[i].Targets[j])
		}
		exp(&d.MemoryInjections[i].Anchor)
	}
	for i := range d.SlashCommands {
		exp(&d.SlashCommands[i].ID)
		exp(&d.SlashCommands[i].Target)
	}
	if d.Verification != nil {
		for j := range d.Verification.RequiredFiles {
			exp(&d.Verification.RequiredFiles[j])
		}
		for j := range d.Verification.CSVContains {
			exp(&d.Verification.CSVContains[j].Target)
			exp(&d.Verification.CSVContains[j].Needle)
		}
	}
	return err
}

// Apply runs the declaration against repoRoot.
//
// Order is fixed rather than declaration order, because the operations have
// real dependencies: CSV first so the agent is registered before later steps
// reference it. This matches the numbered section order all five shell scripts
// converged on independently.
//
// A failure does not abort the remaining operations. A missing bmad-help.csv
// should not prevent the memories from being injected. Failures accumulate and
// are reported together.
func Apply(d *Declaration, repoRoot string, opts Options) (Result, error) {
	var res Result

	res.Actions = append(res.Actions, applyCSVInjections(d, repoRoot, opts)...)
	res.Actions = append(res.Actions, applyMemoryInjections(d, repoRoot, opts)...)
	res.Actions = append(res.Actions, applySlashCommands(d, repoRoot, opts)...)
	res.Actions = append(res.Actions, applyUpstreamPatches(d, repoRoot, opts)...)
	res.Actions = append(res.Actions, runVerification(d, repoRoot, opts)...)

	failures := res.Failures()
	if len(failures) > 0 && opts.Strict {
		return res, fmt.Errorf("weave: %d operation(s) failed", len(failures))
	}
	return res, nil
}

// DeclarationPath returns the conventional weave.yaml location for a pack.
func DeclarationPath(repoRoot, packName string) string {
	return filepath.Join(repoRoot, "_"+packName+"-custom", "weave.yaml")
}

// ApplyForPack loads and applies the declaration for one pack, if present.
// A repo with no weave.yaml is the common case and is not an error.
func ApplyForPack(repoRoot, packName string, opts Options) (Result, bool, error) {
	path := DeclarationPath(repoRoot, packName)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Result{}, false, nil
		}
		return Result{}, false, err
	}
	d, err := Load(path)
	if err != nil {
		return Result{}, true, err
	}
	if d.Pack != packName {
		return Result{}, true, fmt.Errorf("%s declares pack %q, expected %q",
			path, d.Pack, packName)
	}
	res, err := Apply(d, repoRoot, opts)
	return res, true, err
}
