/*******************************************************************************
*
* Copyright 2019-2020 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package httpext

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sapcc/go-bits/logg"
)

var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// ShutdownTimeout is the timeout that ListenAndServeContext() will impose on
// server.Shutdown() before forcefully terminating request handlers that are
// still in progress.
//
// The default timeout is quite lenient to accommodate long-running requests,
// but it can be lowered for servers running in an interactive terminal session
// where a quick response to Ctrl-C is more important.
var ShutdownTimeout = 30 * time.Second

// ContextWithSIGINT creates a new context.Context using the provided Context, and
// launches a goroutine that cancels the Context when an interrupt signal is caught.
//
// This function is not strictly related to net/http, but fits nicely with func
// ListenAndServeContext from this package.
//
// If `delay` is not 0, the context will be canceled with this delay after an
// interrupt signal was caught. This is useful when using the context with
// ListenAndServeContext(), to give reverse-proxies using this HTTP server some
// extra delay to notice the pending shutdown of this server.
func ContextWithSIGINT(ctx context.Context, delay time.Duration) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, shutdownSignals...)
	go func() {
		<-signalChan
		logg.Info("Interrupt received...")
		signal.Reset(shutdownSignals...)
		time.Sleep(delay)
		close(signalChan)
		cancel()
	}()
	return ctx
}

// ListenAndServeContext is a wrapper around http.ListenAndServe() that additionally
// shuts down the HTTP server gracefully when the context expires, or if an error occurs.
func ListenAndServeContext(ctx context.Context, addr string, handler http.Handler) error {
	logg.Info("Listening on %s...", addr)
	server := &http.Server{Addr: addr, Handler: handler}
	return listenAndServeContext(ctx, server, func() error { return server.ListenAndServe() })
}

// ListenAndServeTLSContext is a wrapper around http.ListenAndServeTLS() that additionally
// shuts down the HTTP server gracefully when the context expires, or if an error occurs.
func ListenAndServeTLSContext(ctx context.Context, addr, certFile, keyFile string, handler http.Handler) error {
	logg.Info("Listening on %s...", addr)
	server := &http.Server{Addr: addr, Handler: handler}
	return listenAndServeContext(ctx, server, func() error { return server.ListenAndServeTLS(certFile, keyFile) })
}

func listenAndServeContext(ctx context.Context, server *http.Server, listenAndServe func() error) error {
	// waitForServerShutdown channel serves two purposes:
	// 1. It is used to block until server.Shutdown() returns to prevent
	// program from exiting prematurely. This is because when Shutdown is
	// called ListenAndServe immediately return ErrServerClosed.
	// 2. It is used to convey errors encountered during Shutdown from the
	// goroutine to the caller function.
	waitForServerShutdown := make(chan error)
	shutdownServer := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-shutdownServer:
		}

		logg.Info("Shutting down HTTP server...")

		ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		err := server.Shutdown(ctx)
		cancel()
		waitForServerShutdown <- err
	}()

	listenAndServeErr := listenAndServe()
	if listenAndServeErr != http.ErrServerClosed { //nolint:errorlint // errorlint usually understands that this error is returned directly from ListenAndServe(), but the indirection confuses it
		shutdownServer <- struct{}{}
	}

	shutdownErr := <-waitForServerShutdown
	if listenAndServeErr == http.ErrServerClosed { //nolint:errorlint // same as above
		return addPrefix(shutdownErr, "ListenAndServeContext: could not shutdown HTTP server")
	}

	if shutdownErr != nil {
		logg.Error("Additional error encountered while shutting down server: %s", shutdownErr.Error())
	}
	return addPrefix(listenAndServeErr, "ListenAndServeContext failed")
}

func addPrefix(err error, prefix string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
