//go:build dev

package locktivity

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

// newHTTPTransport returns a transport that resolves .localhost domains,
// which Go's resolver doesn't handle automatically like browsers do.
func newHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if strings.HasSuffix(host, ".localhost") || host == "localhost" {
				addr = net.JoinHostPort("127.0.0.1", port)
			}
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			return dialer.DialContext(ctx, network, addr)
		},
	}
}
