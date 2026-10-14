// Package render prints a Report: styled text for people, JSON for tools.
package render

import (
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
	"github.com/ag3ntsc4rn/lagdoc/internal/rules"
)

// Report is what both renderers print.
type Report struct {
	TakenAt  time.Time
	Host     collect.Host
	Findings []rules.Finding
}

func NewReport(s collect.Snapshot) Report {
	return Report{TakenAt: s.TakenAt, Host: s.Host, Findings: rules.Evaluate(s)}
}
