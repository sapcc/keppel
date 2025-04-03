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

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/easypg"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/mock"
	"github.com/sapcc/go-bits/osext"
	"golang.org/x/crypto/bcrypt"

	authapi "github.com/sapcc/keppel/internal/api/auth"
	keppelv1 "github.com/sapcc/keppel/internal/api/keppel"
	peerv1 "github.com/sapcc/keppel/internal/api/peer"
	registryv2 "github.com/sapcc/keppel/internal/api/registry"
	"github.com/sapcc/keppel/internal/drivers/basic"
	"github.com/sapcc/keppel/internal/drivers/trivial"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
	"github.com/sapcc/keppel/internal/trivy"
)

type setupParams struct {
	// all false/empty by default
	IsSecondary             bool
	WithAnycast             bool
	WithKeppelAPI           bool
	WithPeerAPI             bool
	WithTrivyDouble         bool
	WithQuotas              bool
	WithPreviousIssuerKey   bool
	WithoutCurrentIssuerKey bool
	RateLimitEngine         *keppel.RateLimitEngine
	SetupOfPrimary          *Setup
	Accounts                []*models.Account
	Repos                   []*models.Repository
}

// SetupOption is an option that can be given to NewSetup().
type SetupOption func(*setupParams)

// IsSecondaryTo is a SetupOption that configures registry-secondary.example.org
// instead of registry.example.org. If a non-nil Setup instance is given, that's
// the Setup for the corresponding primary instance, and both sides will be
// configured to peer with each other.
func IsSecondaryTo(s *Setup) SetupOption {
	return func(params *setupParams) {
		params.IsSecondary = true
		params.SetupOfPrimary = s
	}
}

// WithAnycast is a SetupOption that fills the anycast fields in keppel.Configuration if true is given.
func WithAnycast(withAnycast bool) SetupOption {
	return func(params *setupParams) {
		params.WithAnycast = withAnycast
	}
}

// WithKeppelAPI is a SetupOption that enables the Keppel API.
func WithKeppelAPI(params *setupParams) {
	params.WithKeppelAPI = true
}

// WithPeerAPI is a SetupOption that enables the peer API.
func WithPeerAPI(params *setupParams) {
	params.WithPeerAPI = true
}

// WithTrivyDouble is a SetupOption that sets up a TrivyDouble at trivy.example.org.
func WithTrivyDouble(params *setupParams) {
	params.WithTrivyDouble = true
}

// WithQuotas is a SetupOption that sets up ample quota for all configured accounts.
func WithQuotas(params *setupParams) {
	params.WithQuotas = true
}

// WithRateLimitEngine is a SetupOption to use a RateLimitEngine in enabled APIs.
func WithRateLimitEngine(rle *keppel.RateLimitEngine) SetupOption {
	return func(params *setupParams) {
		params.RateLimitEngine = rle
	}
}

// WithAccount is a SetupOption that adds the given keppel.Account to the DB during NewSetup().
func WithAccount(account models.Account) SetupOption {
	return func(params *setupParams) {
		// some field have default values that's not the zero value
		if account.GCPoliciesJSON == "" {
			account.GCPoliciesJSON = "[]"
		}
		if account.SecurityScanPoliciesJSON == "" {
			account.SecurityScanPoliciesJSON = "[]"
		}
		params.Accounts = append(params.Accounts, &account)
	}
}

// WithRepo is a SetupOption that adds the given keppel.Repository to the DB during NewSetup().
func WithRepo(repo models.Repository) SetupOption {
	return func(params *setupParams) {
		params.Repos = append(params.Repos, &repo)
	}
}

// WithPreviousIssuerKey is a SetupOption that will add the "previous" set of test issuer keys.
func WithPreviousIssuerKey(params *setupParams) {
	params.WithPreviousIssuerKey = true
}

// WithoutCurrentIssuerKey is a SetupOption that will not add the "current" set
// of test issuer keys. Tokens will be issued with the "previous" set of issuer
// keys instead, so WithPreviousIssuerKey must be given as well.
func WithoutCurrentIssuerKey(params *setupParams) {
	params.WithoutCurrentIssuerKey = true
}

// Setup contains all the pieces that are needed for most tests.
type Setup struct {
	// fields that are always set
	Config       keppel.Configuration
	DB           *keppel.DB
	Clock        *mock.Clock
	SIDGenerator *StorageIDGenerator
	Auditor      *audittools.MockAuditor
	AD           *AuthDriver
	AMD          *basic.AccountManagementDriver
	FD           *FederationDriver
	SD           *trivial.StorageDriver
	ICD          *InboundCacheDriver
	Handler      http.Handler
	Ctx          context.Context //nolint: containedctx  // only used in tests
	Registry     *prometheus.Registry
	// fields that are only set if the respective With... setup option is included
	TrivyDouble *TrivyDouble
	// fields that are filled by WithAccount and WithRepo (in order)
	Accounts []*models.Account
	Repos    []*models.Repository
	// fields that are only accessible to helper functions
	tokenCache map[string]string
}

// these credentials are in global vars so that we don't have to recompute them
// in every test run (bcrypt is intentionally CPU-intensive)
var (
	replicationPassword     string
	replicationPasswordHash string
)

// GetReplicationPassword returns the password that the secondary registry can
// use to replicate from the primary registry.
func GetReplicationPassword() string {
	if replicationPassword == "" {
		// this password needs to be constant because it appears in some fixtures/*.sql
		replicationPassword = "a4cb6fae5b8bb91b0b993486937103dab05eca93" //nolint:gosec // hardcoded password for test fixtures

		hashBytes, _ := bcrypt.GenerateFromPassword([]byte(replicationPassword), 8) //nolint:errcheck
		replicationPasswordHash = string(hashBytes)
	}
	return replicationPassword
}

// NewSetup prepares most or all pieces of Keppel for a test.
func NewSetup(t *testing.T, opts ...SetupOption) Setup {
	t.Helper()
	logg.ShowDebug = osext.GetenvBool("KEPPEL_DEBUG")
	var params setupParams
	for _, option := range opts {
		option(&params)
	}

	// choose identity
	apiPublicHostname := "registry.example.org"
	if params.IsSecondary {
		apiPublicHostname = "registry-secondary.example.org"
	}

	// build keppel.Configuration
	s := Setup{
		Config: keppel.Configuration{
			APIPublicHostname: apiPublicHostname,
		},
		Ctx:        t.Context(),
		Registry:   prometheus.NewPedanticRegistry(),
		tokenCache: make(map[string]string),
	}

	// select issuer keys
	if params.WithoutCurrentIssuerKey && !params.WithPreviousIssuerKey {
		t.Fatal("test.WithoutCurrentIssuerKey requires test.WithPreviousIssuerKey")
	}
	if params.WithPreviousIssuerKey {
		key, err := keppel.ParseIssuerKey(UnitTestIssuerRSAPrivateKey)
		mustDo(t, err)
		s.Config.JWTIssuerKeys = append(s.Config.JWTIssuerKeys, key)
	}
	if !params.WithoutCurrentIssuerKey {
		jwtIssuerKey, err := keppel.ParseIssuerKey(UnitTestIssuerEd25519PrivateKey)
		mustDo(t, err)
		s.Config.JWTIssuerKeys = append(s.Config.JWTIssuerKeys, jwtIssuerKey)
	}

	if params.WithTrivyDouble {
		s.TrivyDouble = NewTrivyDouble()
		trivyURL, err := url.Parse("https://trivy.example.org/")
		if err != nil {
			t.Fatal(err)
		}

		s.Config.Trivy = &trivy.Config{
			URL: *trivyURL,
		}
		if tt, ok := http.DefaultTransport.(*RoundTripper); ok {
			tt.Handlers[trivyURL.Host] = httpapi.Compose(s.TrivyDouble)
		}
	}

	// connect to DB
	dbOpts := []easypg.TestSetupOption{
		// manifest_manifest_refs needs a specialized cleanup strategy because of an "ON DELETE RESTRICT" constraint
		easypg.ClearContentsWith(`DELETE FROM manifest_manifest_refs WHERE parent_digest NOT IN (SELECT child_digest FROM manifest_manifest_refs)`),
		easypg.ClearTables("manifest_blob_refs", "accounts", "peers", "quotas"),
		easypg.ResetPrimaryKeys("blobs", "repos"),
	}
	if params.IsSecondary {
		dbOpts = append(dbOpts, easypg.OverrideDatabaseName(t.Name()+"_secondary"))
	}
	s.DB = keppel.InitORM(easypg.ConnectForTest(t, keppel.DBConfiguration(), dbOpts...))

	// setup anycast if requested
	if params.WithAnycast {
		s.Config.AnycastAPIPublicHostname = "registry-global.example.org"

		if params.WithPreviousIssuerKey {
			key, err := keppel.ParseIssuerKey(UnitTestAnycastIssuerRSAPrivateKey)
			mustDo(t, err)
			s.Config.AnycastJWTIssuerKeys = append(s.Config.AnycastJWTIssuerKeys, key)
		}
		if !params.WithoutCurrentIssuerKey {
			jwtIssuerKey, err := keppel.ParseIssuerKey(UnitTestAnycastIssuerEd25519PrivateKey)
			mustDo(t, err)
			s.Config.AnycastJWTIssuerKeys = append(s.Config.AnycastJWTIssuerKeys, jwtIssuerKey)
		}
	}

	// setup essential test doubles
	s.Clock = mock.NewClock()
	s.SIDGenerator = &StorageIDGenerator{}
	s.AMD = &basic.AccountManagementDriver{}
	s.Auditor = audittools.NewMockAuditor()

	// if we are secondary and we know the primary, share the clock with it
	if params.SetupOfPrimary != nil {
		s.Clock = params.SetupOfPrimary.Clock
	}

	// setup essential drivers
	ad, err := keppel.NewAuthDriver(s.Ctx, "unittest", nil)
	mustDo(t, err)
	s.AD = ad.(*AuthDriver)
	fd, err := keppel.NewFederationDriver(s.Ctx, "unittest", ad, s.Config)
	mustDo(t, err)
	s.FD = fd.(*FederationDriver)
	sd, err := keppel.NewStorageDriver("in-memory-for-testing", ad, s.Config)
	mustDo(t, err)
	s.SD = sd.(*trivial.StorageDriver)
	icd, err := keppel.NewInboundCacheDriver(s.Ctx, "unittest", s.Config)
	mustDo(t, err)
	s.ICD = icd.(*InboundCacheDriver)

	if params.RateLimitEngine != nil {
		sr := miniredis.RunT(t)
		s.Clock.AddListener(sr.SetTime)
		params.RateLimitEngine.Client = redis.NewClient(&redis.Options{
			Addr: sr.Addr(),
			// SETINFO not supported by miniredis
			DisableIdentity: true,
		})
	}

	// setup APIs
	apis := []httpapi.API{
		httpapi.WithoutLogging(),
		// Registry API (and thus Auth API) are nearly always needed for
		// Bytes.Upload, Image.Upload and ImageList.Upload
		registryv2.NewAPI(s.Config, ad, fd, sd, icd, s.DB, s.Auditor, params.RateLimitEngine).OverrideTimeNow(s.Clock.Now).OverrideGenerateStorageID(s.SIDGenerator.Next),
		authapi.NewAPI(s.Config, ad, fd, s.DB),
	}
	if params.WithKeppelAPI {
		apis = append(apis, keppelv1.NewAPI(s.Config, ad, fd, sd, icd, s.DB, s.Auditor, params.RateLimitEngine).OverrideTimeNow(s.Clock.Now))
	}
	if params.WithPeerAPI {
		apis = append(apis, peerv1.NewAPI(s.Config, ad, s.DB))
	}
	s.Handler = httpapi.Compose(apis...)
	if tt, ok := http.DefaultTransport.(*RoundTripper); ok {
		// make our own API reachable to other peers
		tt.Handlers[s.Config.APIPublicHostname] = s.Handler
		// if accounts are being set up, also expose their domain-remapped APIs
		for _, account := range params.Accounts {
			tt.Handlers[fmt.Sprintf("%s.%s", account.Name, s.Config.APIPublicHostname)] = s.Handler
		}
	}

	// setup initial accounts/repos
	quotasSetFor := make(map[string]bool)
	for _, account := range params.Accounts {
		mustDo(t, s.DB.Insert(account))
		fd.RecordExistingAccount(s.Ctx, *account, s.Clock.Now()) //nolint:errcheck
		if params.WithQuotas && !quotasSetFor[account.AuthTenantID] {
			mustDo(t, s.DB.Insert(&models.Quotas{
				AuthTenantID:  account.AuthTenantID,
				ManifestCount: 100,
			}))
			quotasSetFor[account.AuthTenantID] = true
		}
	}
	s.Accounts = params.Accounts
	for _, repo := range params.Repos {
		mustDo(t, s.DB.Insert(repo))
	}
	s.Repos = params.Repos

	// setup peering with primary if requested
	if params.IsSecondary {
		s1 := params.SetupOfPrimary
		if s1 != nil {
			// give the secondary registry credentials for replicating from the primary
			mustDo(t, s.DB.Insert(&models.Peer{
				HostName:             "registry.example.org",
				UseForPullDelegation: true,
				OurPassword:          GetReplicationPassword(),
			}))
			mustDo(t, s1.DB.Insert(&models.Peer{
				HostName:                 "registry-secondary.example.org",
				TheirCurrentPasswordHash: replicationPasswordHash,
			}))
		}
	}

	return s
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err.Error())
	}
}
