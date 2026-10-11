package testenv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

// Offline keeps the package's tests off the network: http.DefaultTransport
// (and so http.DefaultClient, and every transport cloned from it after the
// call) dials this computer only — localhost, a name under .localhost, a
// loopback or unspecified address — and refuses anything else before it is
// looked up, saying so on stderr once per address. Ambient proxy variables
// are cleared before Go can cache them; a proxy on loopback cannot carry
// a default client's request to an external host either. A test that wants a
// vendor serves it on loopback and points the request there (an
// httptest.Server, a Transport rewriting the host). Without it the tests
// of internal/gateway and internal/provider asked the real models.dev (its
// catalog, fetched as each plugin host started), api.github.com,
// chatgpt.com and Devin's server.codeium.com with made-up accounts, and a
// plan key with a vendor's real base URL would have its windows read for
// routing (#1016). A package calls it from its TestMain, before m.Run.
func Offline() {
	ClearProxy()
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return
	}
	dial := t.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	var mu sync.Mutex
	said := map[string]bool{}
	refused := func(addr string) error {
		mu.Lock()
		if !said[addr] {
			said[addr] = true
			fmt.Fprintf(os.Stderr, "testenv: a test asked %s, refused: tests stay off the network\n", addr)
		}
		mu.Unlock()
		return fmt.Errorf("testenv: %s is not this computer; tests stay off the network", addr)
	}
	t.CloseIdleConnections()
	if proxy := t.Proxy; proxy != nil {
		t.Proxy = func(r *http.Request) (*url.URL, error) {
			p, err := proxy(r)
			if err == nil && p != nil && !local(r.URL.Host) {
				return nil, refused(r.URL.Host)
			}
			return p, err
		}
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if local(addr) {
			return dial(ctx, network, addr)
		}
		return nil, refused(addr)
	}
}

// local says addr, a host:port, is this computer, without looking it up.
func local(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}
