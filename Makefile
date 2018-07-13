PKG     = github.com/sapcc/keppel
PREFIX := /usr

GO            := GOPATH=$(CURDIR)/.gopath GOBIN=$(CURDIR)/build go
GO_BUILDFLAGS :=
GO_LDFLAGS    := -s -w -X $(PKG)/pkg/version.Version=$(shell util/find_version.sh)

build_all: $(patsubst cmd/%/main.go,build/%,$(wildcard cmd/*/main.go))
build/%: FORCE
	$(GO) install $(GO_BUILDFLAGS) -ldflags '$(GO_LDFLAGS)' '$(PKG)/cmd/$*'

run-api: build/keppel-api
	bash -c "source .env && ./build/keppel-api"

.PHONY: FORCE
