package rules

import (
	"fmt"
	"time"

	"github.com/ag3ntsc4rn/lagdoc/internal/collect"
)

func Network(s collect.Snapshot) Finding {
	n := s.Network
	if n.DialAddr == "" {
		return unknown(CheckNetwork, s, "network")
	}
	f := Finding{Check: CheckNetwork, Severity: OK}
	dnsFailed, dialFailed := n.DNSErr != "", n.DialErr != ""
	switch {
	case dnsFailed && dialFailed:
		f.Severity, f.Summary = Crit, "offline"
	case dialFailed:
		f.Severity, f.Summary = Crit, "can't reach the internet"
	case dnsFailed:
		f.Severity, f.Summary = Crit, "dns not answering"
	case n.DNS >= DNSWarn && n.Dial >= DialWarn:
		f.Severity, f.Summary = Warn, fmt.Sprintf("slow · dns %s · connect %s", ms(n.DNS), ms(n.Dial))
	case n.DNS >= DNSWarn:
		f.Severity, f.Summary = Warn, fmt.Sprintf("slow dns %s", ms(n.DNS))
	case n.Dial >= DialWarn:
		f.Severity, f.Summary = Warn, fmt.Sprintf("slow connect %s", ms(n.Dial))
	default:
		f.Summary = fmt.Sprintf("dns %s", ms(n.DNS))
	}
	if dnsFailed {
		f.Details = append(f.Details, fmt.Sprintf("looking up %s failed", n.DNSHost))
	} else {
		f.Details = append(f.Details, fmt.Sprintf("%s resolved in %s", n.DNSHost, ms(n.DNS)))
	}
	if dialFailed {
		f.Details = append(f.Details, fmt.Sprintf("no connection to %s", n.DialAddr))
	} else {
		f.Details = append(f.Details, fmt.Sprintf("%s answered in %s", n.DialAddr, ms(n.Dial)))
	}
	return f
}

func ms(d time.Duration) string {
	return fmt.Sprintf("%d ms", d.Round(time.Millisecond).Milliseconds())
}
