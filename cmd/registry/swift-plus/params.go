/*******************************************************************************
*
* Copyright 2018 SAP SE
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

package swiftplus

import (
	"fmt"

	"github.com/mitchellh/mapstructure"
)

const (
	// defaultChunkSize defines the default size of a segment
	defaultChunkSize = 20 << 20
	// minChunkSize defines the minimum size of a segment
	minChunkSize = 1 << 20
)

// Parameters encapsulates all of the driver parameters after all values have been set
type Parameters struct {
	AuthURL            string
	Username           string
	Password           string
	UserDomainName     string
	UserDomainID       string
	ProjectName        string
	ProjectID          string
	ProjectDomainName  string
	ProjectDomainID    string
	InsecureSkipVerify bool
	RegionName         string
	EndpointType       string
	Container          string
	ObjectPrefix       string
	SecretKey          string
	ChunkSize          int
	PostgresURI        string
}

// FromParameters constructs a new "swift-plus" driver with a given
// parameters map.
// Required parameters:
// - username
// - password
// - authurl
// - container
// - postgresuri
func FromParameters(parameters map[string]interface{}) (*Driver, error) {
	params, err := parseParameters(parameters)
	if err != nil {
		return nil, err
	}

	return NewDriver(params)
}

func parseParameters(in map[string]interface{}) (Parameters, error) {
	params := Parameters{
		InsecureSkipVerify: false,
		EndpointType:       "public",
		ChunkSize:          defaultChunkSize,
	}

	// Sanitize some entries before trying to decode parameters with mapstructure
	// TenantID and Tenant when integers only and passed as ENV variables
	// are considered as integer and not string. The parser fails in this
	// case.
	_, ok := in["projectname"]
	if ok {
		in["projectname"] = fmt.Sprint(in["projectname"])
	}
	_, ok = in["projectid"]
	if ok {
		in["projectid"] = fmt.Sprint(in["projectid"])
	}

	if err := mapstructure.Decode(in, &params); err != nil {
		return Parameters{}, err
	}

	if params.PostgresURI == "" {
		return Parameters{}, fmt.Errorf("No postgresuri parameter provided")
	}

	if params.Username == "" {
		return Parameters{}, fmt.Errorf("No username parameter provided")
	}

	if params.Password == "" {
		return Parameters{}, fmt.Errorf("No password parameter provided")
	}

	if params.AuthURL == "" {
		return Parameters{}, fmt.Errorf("No authurl parameter provided")
	}

	if params.Container == "" {
		return Parameters{}, fmt.Errorf("No container parameter provided")
	}

	if params.ChunkSize < minChunkSize {
		return Parameters{}, fmt.Errorf("The chunksize %#v parameter should be a number that is larger than or equal to %d", params.ChunkSize, minChunkSize)
	}

	return params, nil
}
