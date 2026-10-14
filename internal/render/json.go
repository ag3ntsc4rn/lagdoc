package render

import (
	"encoding/json"
	"io"
	"math"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// SchemaVersion changes only when a field is removed or changes meaning.
// Adding fields keeps the version.
const SchemaVersion = 1

type jsonReport struct {
	SchemaVersion int             `json:"schema_version"`
	TakenAt       time.Time       `json:"taken_at"`
	Host          jsonHost        `json:"host"`
	Verdict       string          `json:"verdict"` // ok | warn | slow | unknown
	Causes        int             `json:"causes"`
	Findings      []rules.Finding `json:"findings"`
}

type jsonHost struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	OSName        string `json:"os_name"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	CPUs          int    `json:"cpus"`
}

func hostJSON(h collect.Host) jsonHost {
	return jsonHost{
		Hostname:      h.Hostname,
		OS:            h.OS,
		OSName:        h.OSName,
		UptimeSeconds: int64(h.Uptime.Seconds()),
		CPUs:          h.CPUs,
	}
}

// JSON writes the report with no styling. Findings keep their ranked order;
// details are always an array and percentages have one decimal place, so
// the output is easy to diff and to parse.
func JSON(w io.Writer, r Report) error {
	fs := make([]rules.Finding, len(r.Findings))
	for i, f := range r.Findings {
		if f.Details == nil {
			f.Details = []string{}
		}
		if f.Percent != nil {
			p := math.Round(*f.Percent*10) / 10
			f.Percent = &p
		}
		fs[i] = f
	}
	out := jsonReport{
		SchemaVersion: SchemaVersion,
		TakenAt:       r.TakenAt.UTC().Truncate(time.Second),
		Host:          hostJSON(r.Host),
		Verdict:       rules.Verdict(fs),
		Causes:        rules.Causes(fs),
		Findings:      fs,
	}
	return writeJSON(w, out)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
