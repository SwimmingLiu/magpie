package testenv

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLocal(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:80": true, "[::1]:443": true, "localhost:3425": true, "LOCALHOST.:1": true,
		"magpie.localhost:80": true, "0.0.0.0:80": true, "[::]:80": true, "127.8.0.1:9": true,
		"models.dev:443": false, "api.kimi.com:443": false, "10.0.0.132:22": false,
		"192.168.1.2:80": false, "localhost.example.com:80": false, "decide.test:443": false,
	} {
		if got := local(addr); got != want {
			t.Errorf("local(%q) = %v, want %v", addr, got, want)
		}
	}
}

// Offline lets a loopback server answer through http.DefaultClient and
// refuses a vendor's host without dialing it.
func TestOffline(t *testing.T) {
	tr := http.DefaultTransport.(*http.Transport)
	was, wasProxy := tr.DialContext, tr.Proxy
	for _, key := range proxyVars {
		t.Setenv(key, "")
	}
	t.Cleanup(func() { tr.DialContext, tr.Proxy = was, wasProxy })
	Offline()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("here")) }))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("loopback refused: %v", err)
	}
	resp.Body.Close()
	clone := tr.Clone() // a transport made from it after the call
	for _, c := range []*http.Client{http.DefaultClient, {Transport: clone}} {
		resp, err := c.Get("https://models.invalid/api.json")
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "tests stay off the network") {
			t.Errorf("models.invalid: %v", err)
		}
	}
}

// A loopback proxy must not turn an external request into a local dial
// that gets past Offline. Neither client may send the request to it.
func TestOfflineRefusesAnExternalHostThroughLocalProxy(t *testing.T) {
	var asked atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.Write([]byte("proxy"))
	}))
	defer proxy.Close()
	pu, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyURL(pu)
	was := http.DefaultTransport
	http.DefaultTransport = tr
	for _, key := range proxyVars {
		t.Setenv(key, "")
	}
	t.Cleanup(func() { http.DefaultTransport = was; tr.CloseIdleConnections() })
	Offline()
	clone := tr.Clone()
	defer clone.CloseIdleConnections()
	for _, transport := range []*http.Transport{tr, clone} {
		resp, err := (&http.Client{Transport: transport}).Get("http://vendor.invalid/test")
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "tests stay off the network") {
			t.Errorf("external request through a local proxy: %v", err)
		}
	}
	if n := asked.Load(); n != 0 {
		t.Errorf("the proxy received %d external requests", n)
	}
}
