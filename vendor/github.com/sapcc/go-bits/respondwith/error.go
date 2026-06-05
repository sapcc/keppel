// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package respondwith

import (
	"net/http"
)

func analyzeError(err error) (message string, status int, hdr http.Header) {
	switch err := err.(type) { //nolint:errorlint // wrapped errors are intentionally ignored, see doc on func CustomStatus()
	case errorWithCustomStatus:
		return err.Inner.Error(), err.Status, err.Header
	default:
		return err.Error(), http.StatusInternalServerError, nil
	}
}

// CustomStatus wraps an error so that, when it is given to respondwith.ErrorText() or respondwith.ObfuscatedErrorText(),
// a response with a different status than the default of 500 (Internal Server Error) will be produced.
// If the status is in the 400..499 range, respondwith.ObfuscatedErrorText() will skip obfuscation,
// since client errors are intended to tell the client what they did wrong.
//
// CustomStatus is usually used by helper functions that are directly called by HTTP request handlers, for example:
//
//	func (a *API) LoadRequestedThingyFromDB(r *http.Request) (db.Thingy, error) {
//		name := r.PathValue("record_name")
//		var t db.Thingy
//		err := a.db.SelectOne(&t, `SELECT * FROM thingies WHERE name = $1`, name)
//		switch {
//		case errors.Is(err, sql.ErrNoRows):
//			// thingy not found -> render a 404 response (without obfuscation)
//			return respondwith.CustomStatus(http.StatusNotFound, fmt.Errorf("no thingy found with name %q", name))
//		case err != nil:
//			// other DB error -> render a 500 response (with obfuscation)
//			return db.Thingy{}, err
//		default:
//			// success
//			return t, nil
//		}
//	}
//
//	func (a *API) HandleGetThingy(w http.ResponseWriter, r *http.Request) {
//		// ...
//
//		thingy, err := a.LoadRequestedThingyFromDB(r)
//		if respondwith.ObfuscatedErrorText(w, err) {
//			return
//		}
//
//		// ...
//	}
//
// CustomStatus only works when its result is NOT wrapped in another error.
// When wrapping occurs, only the outermost error gets to decide which response status is sent.
// This rule prevents sensitive data in the outermost error's message from accidentally leaking through respondwith.ObfuscatedErrorText().
//
// CustomStatus panics when called with a nil error or a non-error status (only 400..599 status codes are allowed).
func CustomStatus(status int, inner error, opts ...CustomOption) error {
	if inner == nil {
		panic("CustomStatus called with inner == nil")
	}
	if status < 400 || status >= 600 {
		panic("CustomStatus called with a non-error status (only 400..599 are allowed)")
	}
	result := errorWithCustomStatus{status, make(http.Header), inner}
	for _, opt := range opts {
		opt(&result)
	}
	return result
}

type errorWithCustomStatus struct { //nolint:errname // I won't put "error" at the end because "customStatusHavingError" sounds stupid
	Status int
	Header http.Header
	Inner  error
}

// Error implements the builtin/error interface.
func (e errorWithCustomStatus) Error() string {
	return e.Inner.Error()
}

// Unwrap implements the unnamed interface implied by package errors.
func (e errorWithCustomStatus) Unwrap() error {
	return e.Inner
}

// CustomOption provides additional behavior to func [CustomStatus].
type CustomOption func(*errorWithCustomStatus)

// CustomHeader adds an HTTP header to an error response built by func [CustomStatus].
// For example:
//
//	return respondwith.CustomStatus(
//		http.StatusTooManyRequests,
//		fmt.Errorf("ratelimit exceeded (limit = %d, used = %d, resetAt = %s)",
//			result.Limit, result.Used, result.ResetAt.Format(time.RFC3339)),
//		respondwith.CustomHeader("Retry-After",
//			strconv.Itoa(time.Until(result.ResetAt).Seconds())),
//	)
func CustomHeader(key, value string) CustomOption {
	return func(e *errorWithCustomStatus) {
		e.Header.Add(key, value)
	}
}
