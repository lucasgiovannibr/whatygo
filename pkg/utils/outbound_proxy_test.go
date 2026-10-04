package utils

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// recordingProxy is a proxy that counts what is sent through it.
func recordingProxy(t *testing.T) (*url.URL, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("answered by the proxy"))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u, &hits
}

func useEnvProxy(t *testing.T, u *url.URL) {
	t.Helper()
	old := envProxy
	envProxy = func(*http.Request) (*url.URL, error) { return u, nil }
	t.Cleanup(func() { envProxy = old })
}

func transportOf(c *http.Client) *http.Transport { return c.Transport.(*http.Transport) }

// Through a proxy the connection is made to the proxy, so the destination check saw the proxy's
// address and never the one in the URL: with HTTP_PROXY set, a URL naming the cloud metadata
// endpoint was handed to the proxy and the protection was gone.
func TestRequestSuppliedURLsDoNotGoThroughTheProxyOfTheEnvironment(t *testing.T) {
	t.Setenv("OUTBOUND_PROXY_FROM_ENV", "")
	proxy, hits := recordingProxy(t)
	useEnvProxy(t, proxy)
	// the proxy is on 127.0.0.1: the tests allow private addresses, the metadata one never is
	SetAllowPrivateURLs(true)

	for name, c := range map[string]*http.Client{
		"public":     newClient(5e9, PolicyPublic),
		"webhook":    NewWebhookClient(5e9),
		"publicOnly": newClient(5e9, PolicyPublicOnly),
	} {
		if transportOf(c).Proxy != nil {
			t.Errorf("%s: must not use the proxy of the environment", name)
		}
		resp, err := c.Get("http://169.254.169.254/latest/meta-data/")
		if resp != nil {
			resp.Body.Close()
		}
		var blocked *BlockedAddressError
		if !errors.As(err, &blocked) {
			t.Errorf("%s: the metadata address must be refused by the dialer, got %v", name, err)
		}
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("nothing may reach the proxy, it got %d requests", got)
	}
}

func TestOperatorEndpointsKeepTheProxyOfTheEnvironment(t *testing.T) {
	proxy, _ := recordingProxy(t)
	useEnvProxy(t, proxy)
	for name, c := range map[string]*http.Client{
		"trusted":  newClient(5e9, PolicyAny),
		"fixedURL": newClient(5e9, PolicyAny),
	} {
		if transportOf(c).Proxy == nil {
			t.Errorf("%s: an endpoint the operator configured keeps the proxy of the environment", name)
		}
	}
	if transportOf(FixedURLClient).Proxy == nil {
		t.Error("FixedURLClient must keep it (it is how an egress-only server reaches the internet)")
	}
}

func TestOutboundProxyFromEnvOptIn(t *testing.T) {
	proxy, _ := recordingProxy(t)
	useEnvProxy(t, proxy)

	t.Setenv("OUTBOUND_PROXY_FROM_ENV", "true")
	if !ProxyFromEnvAllowed() {
		t.Fatal("the opt-in must be read")
	}
	if transportOf(newClient(5e9, PolicyPublic)).Proxy == nil {
		t.Fatal("with OUTBOUND_PROXY_FROM_ENV=true the proxy is used")
	}
	t.Setenv("OUTBOUND_PROXY_FROM_ENV", "yes")
	if ProxyFromEnvAllowed() {
		t.Fatal("only \"true\" opts in")
	}
}

func TestWarnIfEnvProxyIgnored(t *testing.T) {
	count := func() int {
		n := 0
		WarnIfEnvProxyIgnored(func(string, ...interface{}) { n++ })
		return n
	}
	for _, name := range []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy"} {
		t.Setenv(name, "")
	}
	t.Setenv("OUTBOUND_PROXY_FROM_ENV", "")
	if count() != 0 {
		t.Fatal("nothing to warn about without a proxy in the environment")
	}
	t.Setenv("HTTPS_PROXY", "http://proxy.example:3128")
	if count() != 1 {
		t.Fatal("a proxy that will be ignored must be reported once")
	}
	t.Setenv("OUTBOUND_PROXY_FROM_ENV", "true")
	if count() != 0 {
		t.Fatal("no warning when the proxy is used")
	}
}

func TestWebhookClientPolicyFollowsWebhookAllowPrivate(t *testing.T) {
	t.Setenv("WEBHOOK_ALLOW_PRIVATE", "")
	SetAllowPrivateURLs(false)
	defer SetAllowPrivateURLs(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close() // on 127.0.0.1

	if resp, err := NewWebhookClient(5e9).Get(srv.URL); err != nil {
		t.Fatalf("by default a webhook may go to the internal network: %v", err)
	} else {
		resp.Body.Close()
	}

	t.Setenv("WEBHOOK_ALLOW_PRIVATE", "false")
	_, err := NewWebhookClient(5e9).Get(srv.URL)
	var blocked *BlockedAddressError
	if !errors.As(err, &blocked) {
		t.Fatalf("WEBHOOK_ALLOW_PRIVATE=false restricts webhooks to public addresses, got %v", err)
	}

	// ALLOW_PRIVATE_URLS (for media) does not reopen it
	SetAllowPrivateURLs(true)
	if _, err := NewWebhookClient(5e9).Get(srv.URL); !errors.As(err, &blocked) {
		t.Fatalf("ALLOW_PRIVATE_URLS must not relax the strict webhook policy, got %v", err)
	}
}
