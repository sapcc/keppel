// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httpext

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
)

// WrappedTransport is a wrapper that adds various global behaviors to an
// `http.RoundTripper` such as `http.DefaultTransport`.
type WrappedTransport struct {
	// Lock for updates to this struct's other member fields.
	mutex sync.Mutex
	// A reference to the original RoundTripper.
	original http.RoundTripper
	// This is what we swap in for the original RoundTripper.
	outer *outerRoundTripper
}

// WrapTransport replaces the given `http.RoundTripper` with a wrapped version
// that can be modified using the returned WrappedTransport instance. This
// usually targets `http.DefaultTransport` like this:
//
//	transport := httpext.WrapTransport(&http.DefaultTransport)
//	transport.SetOverrideUserAgent("example", "1.0")
func WrapTransport(transport *http.RoundTripper) *WrappedTransport { //nolint:gocritic // The pointer to an interface type is intentional.
	orig := *transport
	w := &WrappedTransport{
		original: orig,
		outer:    &outerRoundTripper{inner: orig},
	}
	*transport = w.outer
	return w
}

// Attach adds a custom modifier to the DefaultTransport. The provided function
// will be used to wrap the existing RoundTripper instance.
func (w *WrappedTransport) Attach(wrap func(http.RoundTripper) http.RoundTripper) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.outer.inner = wrap(w.outer.inner)
}

// SetInsecureSkipVerify sets the InsecureSkipVerify flag on the inner
// Transport's tls.Config. This flag should only be set for testing, esp. to
// enable capturing of requests made by this application through a tracing
// proxy like mitmproxy.
func (w *WrappedTransport) SetInsecureSkipVerify(insecure bool) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// only change the http.Transport if we have to (this is important because the
	// presence of a custom TLSClientConfig may disable some useful behaviors like
	// HTTP/2-by-default, so we only want to instantiate it if actually necessary)
	orig, ok := w.original.(*http.Transport)
	if !ok {
		panic(fmt.Sprintf("SetInsecureSkipVerify: requires the wrapped RoundTripper to be a *http.DefaultTransport, but is actually a %t", w.original))
	}
	oldInsecure := orig.TLSClientConfig != nil && orig.TLSClientConfig.InsecureSkipVerify
	if oldInsecure == insecure {
		return
	}

	if orig.TLSClientConfig == nil {
		orig.TLSClientConfig = &tls.Config{} //nolint:gosec // only used in HTTP client, where stdlib auto-chooses strong TLS versions
	}
	orig.TLSClientConfig.InsecureSkipVerify = insecure
}

// SetOverrideUserAgent sets a User-Agent header that will be injected into all
// HTTP requests that are made with the http.DefaultTransport. The User-Agent
// string is constructed as "appName/appVersion" from the two provided
// arguments. The arguments usually come from go-api-declarations/bininfo, for example:
//
//	httpext.ModifyDefaultTransport().SetOverrideUserAgent(bininfo.Component(), bininfo.Version())
func (w *WrappedTransport) SetOverrideUserAgent(appName, appVersion string) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if appVersion == "" {
		w.outer.overrideUserAgent = appName
	} else {
		w.outer.overrideUserAgent = fmt.Sprintf("%s/%s", appName, appVersion)
	}
}

// outerRoundTripper is what we actually put into `http.DefaultTransport`. Then
// we can change the inner RoundTripper instance whenever we want without
// having to touch `http.DefaultTransport` again, which is helpful in case a
// different library has wrapped `http.DefaultTransport` again after us (e.g.
// to install a test double).
type outerRoundTripper struct {
	inner             http.RoundTripper
	overrideUserAgent string
}

// RoundTrip implements the http.RoundTripper interface.
func (o *outerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if o.overrideUserAgent != "" {
		r.Header.Set("User-Agent", o.overrideUserAgent)
	}
	return o.inner.RoundTrip(r)
}
