// Package doctor is the read-only health surface over the state
// sideshow itself has written: the pack store, the registry receipts,
// the sync manifest, and the repo-bindings ledger. Every check
// verifies a promise some receipt, ledger row, marker, or doc
// sentence already makes. Doctor proposes; it never mutates.
// Spec: docs/doctor-spec.md. Ticket: aae-orc-xteh.
package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Status is the outcome of one finding.
type Status string

const (
	OK          Status = "ok"
	Warn        Status = "warn"
	Fail        Status = "fail"
	Unavailable Status = "unavailable"
)

// Class separates well-formedness from health. Only structural
// findings may move the exit code (orc rule diagnostic-not-gate):
// structural means sideshow's own records disagree with disk;
// advisory means a health signal a human may have intended.
type Class string

const (
	Structural Class = "structural"
	Advisory   Class = "advisory"
)

// Finding is one check observation.
type Finding struct {
	Layer   int    `json:"layer"`
	ID      string `json:"id"`
	Pack    string `json:"pack,omitempty"`
	Subject string `json:"subject,omitempty"`
	Status  Status `json:"status"`
	Class   Class  `json:"class"`
	Detail  string `json:"detail"`
	// Next names the command to run, the doc to read, or the bd
	// ticket that supplies a missing input. Required on every
	// non-ok finding (enforced by the runner's invariant sweep).
	Next string `json:"next,omitempty"`
}

// Report is a full doctor run.
type Report struct {
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   string    `json:"generated_at"`
	Findings      []Finding `json:"findings"`
	Summary       Summary   `json:"summary"`
}

// Summary carries the per-status counts and the computed exit code.
type Summary struct {
	OK          int `json:"ok"`
	Warn        int `json:"warn"`
	Fail        int `json:"fail"`
	Unavailable int `json:"unavailable"`
	ExitCode    int `json:"exit_code"`
}

const reportSchemaVersion = "0.1.0"

// ExitCode computes the exit policy: 0 unless a structural fail is
// present; strict additionally promotes structural warns. Advisory
// and unavailable findings never affect the exit code, under any
// flag. This is the diagnostic-not-gate rule as code.
func ExitCode(findings []Finding, strict bool) int {
	for _, f := range findings {
		if f.Class != Structural {
			continue
		}
		if f.Status == Fail {
			return 2
		}
		if strict && f.Status == Warn {
			return 2
		}
	}
	return 0
}

// summarize fills the summary block.
func summarize(findings []Finding, strict bool) Summary {
	var s Summary
	for _, f := range findings {
		switch f.Status {
		case OK:
			s.OK++
		case Warn:
			s.Warn++
		case Fail:
			s.Fail++
		case Unavailable:
			s.Unavailable++
		}
	}
	s.ExitCode = ExitCode(findings, strict)
	return s
}

// NewReport assembles the machine report.
func NewReport(findings []Finding, strict bool, now time.Time) Report {
	return Report{
		SchemaVersion: reportSchemaVersion,
		GeneratedAt:   now.UTC().Format(time.RFC3339),
		Findings:      findings,
		Summary:       summarize(findings, strict),
	}
}

// WriteJSON emits the machine report.
func (r Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// layerTitles names the fixed layer vocabulary (orc charter F24).
var layerTitles = map[int]string{
	1: "sideshow-native integrity",
	3: "cwd discoverability (warn-only)",
	4: "fleet drift",
	5: "known defects",
}

// WriteText renders the human report, grouped by layer in fixed
// order, with the layer-2 exclusion explained in place so the
// numbering gap reads as a decision rather than a bug. Write errors
// are discarded: the renderer targets a terminal and the exit code
// carries the verdict.
func (r Report) WriteText(w io.Writer, layers []int) {
	for _, layer := range layers {
		_, _ = fmt.Fprintf(w, "layer %d: %s\n", layer, layerTitles[layer])
		if layer == 3 {
			// Printed once so the exclusion is visible next to its
			// neighbors exactly where a reader would look for layer 2.
			_, _ = fmt.Fprintf(w, "  (layer 2, pack-declared validation, is deferred to the weave engine; aae-orc-a44c)\n")
		}
		any := false
		for _, f := range r.Findings {
			if f.Layer != layer {
				continue
			}
			any = true
			subject := f.Subject
			if f.Pack != "" && subject != "" {
				subject = f.Pack + " " + subject
			} else if f.Pack != "" {
				subject = f.Pack
			}
			if subject != "" {
				_, _ = fmt.Fprintf(w, "  [%-11s] %s: %s: %s\n", f.Status, f.ID, subject, f.Detail)
			} else {
				_, _ = fmt.Fprintf(w, "  [%-11s] %s: %s\n", f.Status, f.ID, f.Detail)
			}
			if f.Next != "" && f.Status != OK {
				_, _ = fmt.Fprintf(w, "                next: %s\n", f.Next)
			}
		}
		if !any {
			_, _ = fmt.Fprintf(w, "  (no findings)\n")
		}
	}
	_, _ = fmt.Fprintf(w, "\n%d ok, %d warn, %d fail, %d unavailable\n",
		r.Summary.OK, r.Summary.Warn, r.Summary.Fail, r.Summary.Unavailable)
}
