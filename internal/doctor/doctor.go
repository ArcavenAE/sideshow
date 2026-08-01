package doctor

import (
	"fmt"
	"sort"
	"time"

	"github.com/ArcavenAE/sideshow/internal/bindings"
	"github.com/ArcavenAE/sideshow/internal/ledger"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

// Options configures a run.
type Options struct {
	// Pack narrows every layer to one pack. Empty means all installed.
	Pack string
	// Layers limits the run. Empty means all of 1, 3, 4, 5.
	Layers []int
	// RepoDir is the layer-3 subject. Empty means the caller's cwd
	// was unavailable and layer 3 reports that.
	RepoDir string
	// Now is injected for testable TTL and timestamp rendering.
	Now time.Time
	// Strict promotes structural warns into the failing exit.
	Strict bool
}

// AllLayers is the fixed layer vocabulary (orc charter F24). Layer 2
// is excluded by decision, not omission; see docs/doctor-spec.md.
var AllLayers = []int{1, 3, 4, 5}

// Context is the preloaded, read-only input set shared by every
// check. Inputs that failed to load carry their error so dependent
// checks report unavailable with the parse error verbatim instead of
// treating absence as cleanliness.
type Context struct {
	Registry    *pack.Registry
	RegistryErr error
	Ledger      *ledger.Ledger
	LedgerErr   error
	Manifest    *bindings.SyncManifest
	ManifestErr error
	// Packs is the installed set after the Options.Pack filter.
	Packs []pack.InstalledPack
	// PackFilter carries Options.Pack for checks that walk inputs
	// keyed by pack name (ledger rows, receipts, sync entries).
	PackFilter string
	// RepoDir is the layer-3 subject directory.
	RepoDir string
	Now     time.Time
}

// Check is one doctor probe: read-only, returning zero or more
// findings. Checks never mutate anything.
type Check struct {
	ID    string
	Layer int
	Run   func(ctx *Context) []Finding
}

// checkSets returns the registered checks. Plugin-class packs get an
// extra set dispatched from the activation contract; new check sets
// (aae-orc-d3nq.9) register here without touching Run, the output
// layout, or the exit policy.
func checkSets() []Check {
	var checks []Check
	checks = append(checks, layer1Checks()...)
	checks = append(checks, pluginClassChecks()...)
	checks = append(checks, layer3Checks()...)
	checks = append(checks, layer4Checks()...)
	checks = append(checks, layer5Checks()...)
	return checks
}

// Run executes the doctor and returns the assembled report plus the
// layer list actually run (for the text renderer's grouping).
func Run(opts Options) (Report, []int, error) {
	layers := opts.Layers
	if len(layers) == 0 {
		layers = AllLayers
	}
	sort.Ints(layers)
	for _, l := range layers {
		switch l {
		case 1, 3, 4, 5:
		case 2:
			return Report{}, nil, fmt.Errorf("layer 2 (pack-declared validation) is deferred to the weave engine; see aae-orc-a44c and docs/doctor-spec.md")
		default:
			return Report{}, nil, fmt.Errorf("unknown layer %d (valid: 1,3,4,5)", l)
		}
	}
	want := make(map[int]bool, len(layers))
	for _, l := range layers {
		want[l] = true
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	ctx := loadContext(opts, now)

	var findings []Finding
	for _, c := range checkSets() {
		if !want[c.Layer] {
			continue
		}
		fs := c.Run(ctx)
		if c.Layer == 3 {
			fs = clampAdvisory(fs)
		}
		findings = append(findings, fs...)
	}
	findings = sweepInvariants(findings)

	return NewReport(findings, opts.Strict, now), layers, nil
}

// loadContext preloads every shared input once, capturing load errors
// per input.
func loadContext(opts Options, now time.Time) *Context {
	ctx := &Context{Now: now, PackFilter: opts.Pack, RepoDir: opts.RepoDir}
	ctx.Registry, ctx.RegistryErr = pack.LoadRegistry()
	ctx.Ledger, ctx.LedgerErr = ledger.Load(ledger.Path())
	ctx.Manifest, ctx.ManifestErr = bindings.LoadManifest()

	if ctx.Registry != nil {
		for _, p := range ctx.Registry.Packs {
			if opts.Pack == "" || p.Name == opts.Pack {
				ctx.Packs = append(ctx.Packs, p)
			}
		}
	}
	return ctx
}

// clampAdvisory enforces the ratified layer-3 decision in code: every
// finding is advisory, and nothing exceeds warn. A check that returns
// fail is a programming error the clamp corrects and names.
func clampAdvisory(fs []Finding) []Finding {
	for i := range fs {
		fs[i].Class = Advisory
		if fs[i].Status == Fail {
			fs[i].Status = Warn
			fs[i].Detail += " (graded fail by the check; clamped to warn: layer 3 is warn-only by decision)"
		}
	}
	return fs
}

// sweepInvariants enforces the next-line invariant: every non-ok
// finding names its next step. A finding without one gets an explicit
// marker so the omission is visible in output and caught by tests,
// never silently rendered as a dead end.
func sweepInvariants(fs []Finding) []Finding {
	for i := range fs {
		if fs[i].Status != OK && fs[i].Next == "" {
			fs[i].Next = "(no next step recorded; report this as a sideshow doctor bug)"
		}
	}
	return fs
}
