/******************************************************************************
*
*  Copyright 2020 SAP SE
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

package keppel

import (
	//distribution.UnmarshalManifest() relies on the following packages
	//registering their manifest schemas.
	_ "github.com/docker/distribution/manifest/manifestlist"
	_ "github.com/docker/distribution/manifest/ocischema"
	_ "github.com/docker/distribution/manifest/schema2"
	//NOTE: We don't enable github.com/docker/distribution/manifest/schema1
	//anymore since it's legacy anyway and the implementation is a lot simpler
	//when we don't have to rewrite manifests between schema1 and schema2.
)
