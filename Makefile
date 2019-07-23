PKG     = github.com/sapcc/keppel
PREFIX := /usr

all: build_all

GO            := GOPATH=$(CURDIR)/.gopath GOBIN=$(CURDIR)/build go
GO_BUILDFLAGS :=
GO_LDFLAGS    := -s -w -X $(PKG)/internal/keppel.Version=$(shell util/find_version.sh)

# These targets use the incremental rebuild capabilities of the Go compiler to
# speed things up. If no source files have changed, `go install` exits quickly
# without doing anything.
build_all: $(patsubst cmd/%/main.go,build/%,$(wildcard cmd/*/main.go))
build/keppel-%: FORCE
	$(GO) install $(GO_BUILDFLAGS) -ldflags '$(GO_LDFLAGS)' '$(PKG)/cmd/keppel-$*'

install: FORCE $(patsubst cmd/%/main.go,install/%,$(wildcard cmd/*/main.go))
install/keppel-%: build/keppel-% FORCE
	install -D -m 0755 build/keppel-$* "$(DESTDIR)$(PREFIX)/bin/keppel-$*"

################################################################################

# This is for manual testing with the "local-processes" orchestrator.
run-api-%: build/keppel-api build/keppel-registry
	env PATH=$(CURDIR)/build:$$PATH keppel-api $*.yaml

################################################################################

# which packages to test with static checkers?
GO_ALLPKGS := $(shell go list $(PKG)/...)
# which packages to test with `go test`?
GO_TESTPKGS := $(shell go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' $(PKG)/internal/...)
# which packages to measure coverage for?
GO_COVERPKGS := $(shell go list $(PKG)/internal/... | grep -v internal/drivers | grep -v internal/registry/swift-plus | grep -v internal/test/util)
# output files from `go test`
GO_COVERFILES := $(patsubst %,build/%.cover.out,$(subst /,_,$(GO_TESTPKGS)))

# down below, I need to substitute spaces with commas; because of the syntax,
# I have to get these separators from variables
space := $(null) $(null)
comma := ,

check: all static-check build/cover.html FORCE
	@echo -e "\e[1;32m>> All tests successful.\e[0m"
static-check: FORCE
	@if ! hash golint 2>/dev/null; then echo ">> Installing golint..."; go get -u github.com/golang/lint/golint; fi
	@echo '>> gofmt'
	@if s="$$(gofmt -s -l *.go internal 2>/dev/null)"                            && test -n "$$s"; then printf ' => %s\n%s\n' gofmt  "$$s"; false; fi
	@echo '>> golint'
	@if s="$$(golint . && find internal -type d ! -name dbdata -exec golint {} \; 2>/dev/null)" && test -n "$$s"; then printf ' => %s\n%s\n' golint "$$s"; false; fi
	@echo '>> go vet'
	@$(GO) vet $(GO_ALLPKGS)
build/%.cover.out: FORCE
	@echo '>> go test $*'
	$(GO) test $(GO_BUILDFLAGS) -ldflags '$(GO_LDFLAGS)' -coverprofile=$@ -covermode=count -coverpkg=$(subst $(space),$(comma),$(GO_COVERPKGS)) $(subst _,/,$*)
build/cover.out: $(GO_COVERFILES)
	internal/test/util/gocovcat.go $(GO_COVERFILES) > $@
build/cover.html: build/cover.out
	$(GO) tool cover -html $< -o $@

.PHONY: FORCE
