/*******************************************************************************
*
* Copyright 2019 SAP SE
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

//Package httpee provides some convenience functions on top of the "http"
//package from the stdlib.
package httpee

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sapcc/go-bits/logg"
)

// ContextWithSIGINT creates a new context.Context using the provided Context, and
// launches a goroutine that cancels the Context when an interrupt signal is caught.
func ContextWithSIGINT(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signalChan
		logg.Info("Interrupt received...")
		signal.Reset(os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
		close(signalChan)
		cancel()
	}()
	return ctx
}

// ListenAndServeContext is a wrapper around http.ListenAndServe() that additionally
// shuts down the HTTP server gracefully when the context expires, or if an error occurs.
func ListenAndServeContext(ctx context.Context, addr string, handler http.Handler) error {
	server := &http.Server{Addr: addr, Handler: handler}

	// waitForServerShutdown channel serves two purposes:
	// 1. It is used to block until server.Shutdown() returns to prevent
	// program from exiting prematurely. This is because when Shutdown is
	// called, Serve, ListenAndServe, and ListenAndServeTLS immediately return
	// ErrServerClosed.
	// 2. It is used to convey errors encountered during Shutdown from the
	// goroutine to the caller function.
	waitForServerShutdown := make(chan error)
	shutdownServer := make(chan int, 1)
	go func() {
		select {
		case <-ctx.Done():
		case <-shutdownServer:
		}

		logg.Info("Shutting down HTTP server...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := server.Shutdown(ctx)
		if err != nil {
			waitForServerShutdown <- err
		}
		close(waitForServerShutdown)
		cancel()
	}()

	listenAndServeErr := server.ListenAndServe()
	if listenAndServeErr != http.ErrServerClosed {
		shutdownServer <- 1
	}

	shutdownErr := <-waitForServerShutdown
	if listenAndServeErr == http.ErrServerClosed {
		return shutdownErr
	}

	if shutdownErr != nil {
		logg.Error("Additional error encountered while shutting down server: %s", shutdownErr.Error())
	}
	return listenAndServeErr
}
