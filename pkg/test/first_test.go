/******************************************************************************
*
*  Copyright 2018 Stefan Majewsky <majewsky@gmx.net>
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package test

import (
	"testing"
)

//TODO replace by more specific tests (this test only exists to test the testing setup)
func TestBasic(t *testing.T) {
	Setup(t, `
		api:
			public_url: https://registry.example.org

		auth: { driver: noop }
		orchestration: { driver: noop }
		storage: { driver: noop }
	`)
}
