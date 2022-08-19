/*******************************************************************************
*
* Copyright 2022 SAP SE
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
	wrap.SetInsecureSkipVerify(osext.GetenvBool("KEPPEL_INSECURE")) //for debugging with mitmproxy etc. (DO NOT SET IN PRODUCTION)
	wrap.SetOverrideUserAgent(bininfo.Component(), bininfo.VersionOr("rolling"))
}

func SetTaskName(taskName string) {
	bininfo.SetTaskName(taskName)
	wrap.SetOverrideUserAgent(bininfo.Component(), bininfo.VersionOr("rolling"))
	logg.Info("starting %s %s", bininfo.Component(), bininfo.VersionOr("rolling"))
}
