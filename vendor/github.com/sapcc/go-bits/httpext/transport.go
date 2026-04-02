// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package httpext

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// TransportOpts contains options for building a *http.Transport object.
type TransportOpts struct {
	// custom tls.Config with fixed server CA cert and/or client cert
	ServerCACertificatePath  string
	ClientCertificatePath    string
	ClientCertificateKeyPath string
}

// NewTransport builds an *http.Transport based on the provided options.
// If no special options are set, this will return an instance
// that is functionally equivalent to the default settings of http.DefaultTransport.
//
// This function should be preferred over constructing a http.Transport by hand,
// because it will account for new fields being added to http.Transport over time.
// For example, http.Transport literals that were written before Go 1.23 will be
// missing the IdleConnTimeout field, thus setting it to 0 which is equivalent
// to "leak a ton of memory". This function has test coverage to decrease the
// chance of this happening... again.
func NewTransport(opts TransportOpts) (*http.Transport, error) {
	if opts.ClientCertificatePath == "" && opts.ClientCertificateKeyPath != "" {
		return nil, errors.New("private key given, but no client certificate given")
	}
	if opts.ClientCertificatePath != "" && opts.ClientCertificateKeyPath == "" {
		return nil, errors.New("client certificate given, but no private key given")
	}

	// This is intended to construct `result` in the same way as net/http.DefaultTransport.
	// If TestDefaultTransport fails, update this paragraph to match the initialization of that variable in std.
	//
	// NOTE: We are not just using http.DefaultTransport.Clone() because:
	//       1) http.DefaultTransport is an http.RoundTripper and may contain a type other than *http.Transport
	//       2) http.Transport.Clone() has known bugs, see <https://github.com/golang/go/issues/39302>
	result := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if opts.ClientCertificatePath != "" || opts.ServerCACertificatePath != "" {
		// only instantiate TLSClientConfig when actually necessary; its presence may disable
		// useful behaviors like HTTP/2-by-default, so it should only be present when necessary
		result.TLSClientConfig = &tls.Config{}
	}

	if opts.ClientCertificatePath != "" {
		clientCert, err := tls.LoadX509KeyPair(opts.ClientCertificatePath, opts.ClientCertificateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("cannot load client certificate from %s and %s: %w",
				opts.ClientCertificatePath, opts.ClientCertificateKeyPath, err)
		}
		result.TLSClientConfig.Certificates = []tls.Certificate{clientCert}
	}

	if opts.ServerCACertificatePath != "" {
		serverCACert, err := os.ReadFile(opts.ServerCACertificatePath)
		if err != nil {
			return nil, fmt.Errorf("cannot load CA certificate from %s: %w",
				opts.ServerCACertificatePath, err)
		}
		certPool := x509.NewCertPool()
		certPool.AppendCertsFromPEM(serverCACert)
		result.TLSClientConfig.RootCAs = certPool
	}

	return result, nil
}

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
		panic(fmt.Sprintf("SetInsecureSkipVerify: requires the wrapped RoundTripper to be a *http.DefaultTransport, but is actually a %T", w.original))
	}
	oldInsecure := orig.TLSClientConfig != nil && orig.TLSClientConfig.InsecureSkipVerify
	if oldInsecure == insecure {
		return
	}

	if orig.TLSClientConfig == nil {
		orig.TLSClientConfig = &tls.Config{}
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
