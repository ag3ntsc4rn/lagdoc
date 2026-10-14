package collect

import (
	"context"
	"net"
	"time"
)

const (
	// DNSProbeHost is resolved through the system resolver, the same path
	// every app uses, so a cached answer is a fair answer.
	DNSProbeHost = "cloudflare.com"
	// DialProbeAddr is Cloudflare's resolver over TCP 443: dialled by IP so
	// reachability is measured separately from DNS.
	DialProbeAddr = "1.1.1.1:443"
	probeTimeout  = 3 * time.Second
)

func probeNetwork(ctx context.Context) Network {
	n := Network{DNSHost: DNSProbeHost, DialAddr: DialProbeAddr}

	dctx, cancel := context.WithTimeout(ctx, probeTimeout)
	start := time.Now()
	_, err := net.DefaultResolver.LookupHost(dctx, DNSProbeHost)
	n.DNS = time.Since(start)
	cancel()
	if err != nil {
		n.DNSErr = err.Error()
	}

	d := net.Dialer{Timeout: probeTimeout}
	start = time.Now()
	conn, err := d.DialContext(ctx, "tcp", DialProbeAddr)
	n.Dial = time.Since(start)
	if err != nil {
		n.DialErr = err.Error()
	} else {
		conn.Close()
	}
	return n
}
