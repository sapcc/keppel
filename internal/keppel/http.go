// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"net/http"

	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/osext"
)

var wrap *httpext.WrappedTransport

func SetupHTTPClient() {
	wrap = httpext.WrapTransport(&http.DefaultTransport)
	wrap.SetInsecureSkipVerify(osext.GetenvBool("KEPPEL_INSECURE")) // for debugging with mitmproxy etc. (DO NOT SET IN PRODUCTION)
	wrap.SetOverrideUserAgent(bininfo.Component(), bininfo.VersionOr("rolling"))
}

func SetTaskName(taskName string) {
	bininfo.SetTaskName(taskName)
	wrap.SetOverrideUserAgent(bininfo.Component(), bininfo.VersionOr("rolling"))
	logg.Info("starting %s %s", bininfo.Component(), bininfo.VersionOr("rolling"))
}
