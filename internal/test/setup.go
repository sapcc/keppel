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
	Auditor      *Auditor
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
	var (
		dbName            string
		apiPublicHostname string
	)
	if params.IsSecondary {
		dbName = "keppel_secondary"
		apiPublicHostname = "registry-secondary.example.org"
	} else {
		dbName = "keppel"
		apiPublicHostname = "registry.example.org"
	}

	// suitable for use with ./testing/with-postgres-db.sh
	postgresURL := fmt.Sprintf("postgres://postgres:postgres@localhost:54321/%s?sslmode=disable", dbName)

	// build keppel.Configuration
	dbURL, err := url.Parse(postgresURL)
	mustDo(t, err)
	s := Setup{
		Config: keppel.Configuration{
			APIPublicHostname: apiPublicHostname,
			DatabaseURL:       dbURL,
		},
		Ctx:        context.Background(),
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
	s.DB, err = keppel.InitDB(s.Config.DatabaseURL)
	if err != nil {
		t.Error(err)
		t.Log("Try prepending ./testing/with-postgres-db.sh to your command.")
		t.FailNow()
	}

	// wipe the DB clean if there are any leftovers from the previous test run,
	// starting with the manifest_manifest_refs table (this table's foreign-key
	// constraints are so entangled that any attempt to cascade a deletion from
	// higher up in the hierarchy will run into some ON DELETE RESTRICT
	// constraints and fail)
	for {
		result, err := s.DB.Exec(`DELETE FROM manifest_manifest_refs WHERE parent_digest NOT IN (SELECT child_digest FROM manifest_manifest_refs)`)
		mustDo(t, err)
		rowsDeleted, err := result.RowsAffected()
		mustDo(t, err)
		if rowsDeleted == 0 {
			break
		}
	}

	// wipe the DB clean if there are any leftovers from the previous test run
	easypg.ClearTables(t, s.DB.Db, "manifest_blob_refs", "accounts", "peers", "quotas")
	easypg.ResetPrimaryKeys(t, s.DB.Db, "blobs", "repos")

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
	s.Auditor = &Auditor{}

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
			DisableIndentity: true,
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

// UnitTestIssuerEd25519PrivateKey is an ed25519 private key that can be used as
// KEPPEL_ISSUER_KEY in unit tests. DO NOT USE IN PRODUCTION.
const UnitTestIssuerEd25519PrivateKey = `-----BEGIN PRIVATE KEY-----
MC4CAQAwBQYDK2VwBCIEIJF8IUp7t4h64Xm9WDPtThzRHiQY5guceFs4z8QDrMQ0
-----END PRIVATE KEY-----`

// UnitTestIssuerRSAPrivateKey is an RSA private key that can be used as
// KEPPEL_ISSUER_KEY in unit tests. DO NOT USE IN PRODUCTION.
//
//nolint:gosec // snakeoil certs for tests
const UnitTestIssuerRSAPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----`

// UnitTestAnycastIssuerEd25519PrivateKey is an ed25519 private key that can be used as
// KEPPEL_ISSUER_KEY in unit tests. DO NOT USE IN PRODUCTION.
const UnitTestAnycastIssuerEd25519PrivateKey = `-----BEGIN PRIVATE KEY-----
MC4CAQAwBQYDK2VwBCIEIMk7vAS28DlAzYWG9yktmmAnla+wvvTo/Ah6qmXG6E+S
-----END PRIVATE KEY-----`

// UnitTestAnycastIssuerRSAPrivateKey is an RSA private key that can be used as
// KEPPEL_ANYCAST_ISSUER_KEY in unit tests. DO NOT USE IN PRODUCTION.
//
//nolint:gosec // snakeoil certs for tests
const UnitTestAnycastIssuerRSAPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----`
