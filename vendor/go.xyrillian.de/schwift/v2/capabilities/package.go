// // SPDX-FileCopyrightText: 2018 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

// Package capabilities contains feature switches that Schwift's unit tests can
// set to exercise certain fallback code paths in Schwift that they could not
// trigger otherwise.
//
// THIS IS A PRIVATE MODULE. It is not covered by any forwards or backwards
// compatibility and may be gone at a moment's notice.
package capabilities

// AllowBulkDelete can be set to false to force Schwift to act as if the server
// does not support bulk deletion.
var AllowBulkDelete = true
