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
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	storagedriver "github.com/docker/distribution/registry/storage/driver"
	"github.com/docker/distribution/version"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/majewsky/schwift"
	"github.com/majewsky/schwift/gopherschwift"
)

type swiftInterface struct {
	Container      *schwift.Container
	ObjectPrefix   string
	ChunkSize      int
	TempURLKey     string
	TempURLMethods []string
}

func newSwiftInterface(params Parameters) (*swiftInterface, error) {
	provider, err := openstack.NewClient(params.AuthURL)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize OpenStack client: %v", err)
	}

	provider.HTTPClient = *http.DefaultClient
	provider.HTTPClient.Transport = &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConnsPerHost: 2048,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: params.InsecureSkipVerify},
	}

	err = openstack.Authenticate(provider, gophercloud.AuthOptions{
		IdentityEndpoint: params.AuthURL,
		AllowReauth:      true,
		Username:         params.Username,
		DomainID:         params.UserDomainID,
		DomainName:       params.UserDomainName,
		Password:         params.Password,
		Scope: &gophercloud.AuthScope{
			ProjectID:   params.ProjectID,
			ProjectName: params.ProjectName,
			DomainID:    params.ProjectDomainID,
			DomainName:  params.ProjectDomainName,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("cannot fetch initial Keystone token: %v", err)
	}

	client, err := openstack.NewObjectStorageV1(provider, gophercloud.EndpointOpts{
		Region:       params.RegionName,
		Availability: gophercloud.Availability(params.EndpointType),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot find Swift in Keystone catalog: %v", err)
	}

	account, err := gopherschwift.Wrap(client, &gopherschwift.Options{
		UserAgent: "distribution/" + version.Version,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot access Swift account: %v", err)
	}

	container, err := account.Container(params.Container).EnsureExists()
	if err != nil {
		return nil, err
	}

	result := &swiftInterface{
		Container:    container,
		ObjectPrefix: params.ObjectPrefix,
		ChunkSize:    params.ChunkSize,
		TempURLKey:   params.SecretKey,
	}

	//check if tempurl is enabled
	capabilities, err := account.Capabilities()
	if err != nil {
		return nil, err
	}
	if capabilities.TempURL != nil {
		result.TempURLMethods = capabilities.TempURL.Methods

		//find tempurl key
		if params.SecretKey == "" {
			hdr, err := container.Headers()
			if err != nil {
				return nil, err
			}
			switch {
			case hdr.TempURLKey().Exists():
				result.TempURLKey = hdr.TempURLKey().Get()
			case hdr.TempURLKey2().Exists():
				result.TempURLKey = hdr.TempURLKey2().Get()
			default:
				//generate tempurl key on first startup
				result.TempURLKey, err = generateSecret()
				if err != nil {
					return nil, err
				}
				hdr := schwift.NewContainerHeaders()
				hdr.TempURLKey().Set(result.TempURLKey)
				err = container.Update(hdr, nil)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	return result, nil
}

func generateSecret() (string, error) {
	var secretBytes [32]byte
	if _, err := rand.Read(secretBytes[:]); err != nil {
		return "", fmt.Errorf("could not generate random bytes for Swift secret key: %v", err)
	}
	return hex.EncodeToString(secretBytes[:]), nil
}

func (s *swiftInterface) Reader(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	opts := schwift.RequestOptions{Context: ctx}
	if offset > 0 {
		opts.Headers = schwift.Headers{"Range": "bytes=" + strconv.FormatInt(offset, 10) + "-"}
	}

	r, err := s.Container.Object(path).Download(&opts).AsReadCloser()
	if schwift.Is(err, http.StatusNotFound) {
		err = storagedriver.PathNotFoundError{Path: path}
	} else if schwift.Is(err, http.StatusRequestedRangeNotSatisfiable) {
		return ioutil.NopCloser(bytes.NewReader(nil)), nil
	}
	return r, err
}

func (s *swiftInterface) Write(ctx context.Context, path string, data []byte) (hash string, e error) {
	md5sum := md5.Sum(data)
	hash = hex.EncodeToString(md5sum[:])

	hdr := schwift.NewObjectHeaders()
	hdr.Etag().Set(hash)
	opts := hdr.ToOpts()
	opts.Context = ctx

	return hash, s.Container.Object(path).Upload(bytes.NewReader(data), nil, opts)
}

func (s *swiftInterface) WriteSLO(ctx context.Context, path string, segments []plusSegment) error {
	lo, err := s.Container.Object(path).AsNewLargeObject(
		schwift.SegmentingOptions{
			Strategy:         schwift.StaticLargeObject,
			SegmentContainer: s.Container, //ignored since we AddSegment() manually
		},
		&schwift.TruncateOptions{DeleteSegments: false},
	)
	if err != nil {
		return err
	}

	for _, segment := range segments {
		err := lo.AddSegment(schwift.SegmentInfo{
			Object:    s.Container.Object(segment.ObjectPath()),
			SizeBytes: uint64(segment.SizeBytes),
			Etag:      segment.Hash,
		})
		if err != nil {
			return err
		}
	}

	return lo.WriteManifest(&schwift.RequestOptions{Context: ctx})
}

func (s *swiftInterface) DeleteAll(ctx context.Context, prefix string) error {
	iter := s.Container.Objects()
	iter.Prefix = prefix
	iter.Options = &schwift.RequestOptions{Context: ctx}
	objs, err := iter.Collect()
	if err != nil {
		return err
	}

	_, _, err = s.Container.Account().BulkDelete(objs, nil,
		&schwift.RequestOptions{Context: ctx},
	)
	return err
}

func (s *swiftInterface) MakeTempURL(ctx context.Context, path string, options map[string]interface{}) (string, error) {
	if s.TempURLKey == "" {
		return "", storagedriver.ErrUnsupportedMethod{}
	}

	var method string
	switch m := options["method"].(type) {
	case nil:
		method = "GET"
	case string:
		method = m
	default:
		return "", storagedriver.ErrUnsupportedMethod{}
	}

	if method == "HEAD" {
		// A "HEAD" request on a temporary URL is allowed if the
		// signature was generated with "GET", "POST" or "PUT"
		method = "GET"
	}

	supported := false
	for _, m := range s.TempURLMethods {
		if m == method {
			supported = true
			break
		}
	}
	if !supported {
		return "", storagedriver.ErrUnsupportedMethod{}
	}

	var expires time.Time
	switch e := options["expiry"].(type) {
	case nil:
		expires = time.Now().Add(20 * time.Minute)
	case time.Time:
		expires = e
	}

	return s.Container.Object(path).TempURL(s.TempURLKey, method, expires)
}
