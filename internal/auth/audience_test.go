/*******************************************************************************
*
* Copyright 2021 SAP SE
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

package auth

import (
	"fmt"
	"testing"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/keppel/internal/keppel"
)

func TestValidAudience(t *testing.T) {
	testCases := []struct {
		Hostname string
		Audience Audience
	}{
		{"registry.example.org", Audience{IsAnycast: false}},
		{"registry-global.example.org", Audience{IsAnycast: true}},
		{"foo.registry.example.org", Audience{IsAnycast: false, AccountName: "foo"}},
		{"foo.registry-global.example.org", Audience{IsAnycast: true, AccountName: "foo"}},
	}

	for _, tc := range testCases {
		//with anycast enabled, parsing and serializing should work exactly as specified in the testcase
		cfg := keppel.Configuration{
			APIPublicHostname:        "registry.example.org",
			AnycastAPIPublicHostname: "registry-global.example.org",
		}
		desc := fmt.Sprintf("parsed audience of %q", tc.Hostname)
		assert.DeepEqual(t, desc, IdentifyAudience(tc.Hostname, cfg), tc.Audience)
		assert.DeepEqual(t, "audience.Hostname()", tc.Audience.Hostname(cfg), tc.Hostname)

		//with anycast disabled, parsing the anycast hostnames will fall back to the default audience
		cfg.AnycastAPIPublicHostname = ""
		desc = fmt.Sprintf("parsed audience of %q with anycast disabled", tc.Hostname)
		if tc.Audience.IsAnycast {
			assert.DeepEqual(t, desc, IdentifyAudience(tc.Hostname, cfg), Audience{IsAnycast: false})
		} else {
			//same as before for non-anycast hostnames
			assert.DeepEqual(t, desc, IdentifyAudience(tc.Hostname, cfg), tc.Audience)
			assert.DeepEqual(t, "audience.Hostname()", tc.Audience.Hostname(cfg), tc.Hostname)
		}
	}
}

func TestInvalidAudience(t *testing.T) {
	cfg := keppel.Configuration{
		APIPublicHostname:        "registry.example.org",
		AnycastAPIPublicHostname: "registry-global.example.org",
	}
	brokenHostnames := []string{
		"",
		".",
		"org",
		"-1-.registry.example.org",
		".registry-global.example.org",
	}

	//all of these should fall back into the default audience instead of
	//generating nonsensical Audience instances
	for _, hostname := range brokenHostnames {
		desc := fmt.Sprintf("parsed audience of %q", hostname)
		assert.DeepEqual(t, desc, IdentifyAudience(hostname, cfg), Audience{IsAnycast: false})
	}
}
