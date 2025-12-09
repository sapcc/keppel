// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package syncext

// Semaphore implements a counting semaphore: When multiple calls to its methods are made concurrently,
// only a maximum number of them may execute at the same time, and additional calls are made to wait until earlier ones finish.
//
// This is most commonly used with expensive operations (either in terms of CPU or RAM) in order to avoid consuming all
// available resources or running into an OOM (Out Of Memory) error.
//
// The implementation is based on <https://eli.thegreenplace.net/2019/on-concurrency-in-go-http-servers/>.
type Semaphore struct {
	ch chan struct{}
}

// NewSemaphore creates a new semaphore that allows up to the given number of concurrent operations.
func NewSemaphore(count int) *Semaphore {
	return &Semaphore{
		ch: make(chan struct{}, count),
	}
}

// Run executes the given function, ensuring that no more than the maximum number of ops for this semaphore execute concurrently.
func (s *Semaphore) Run(action func()) {
	s.ch <- struct{}{}
	defer func() { <-s.ch }()
	action()
}

// RunFallible is like Run, but allows the callback to return an error.
func (s *Semaphore) RunFallible(action func() error) (err error) {
	s.Run(func() { err = action() })
	return
}
