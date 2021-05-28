help:
	@echo 'Available targets:'
	@echo '    make generate'
	@echo '    make test'

GO_BUILDFLAGS =
GO_LDFLAGS =
GO_TESTENV =

################################################################################

generate: generated.go

%: %.in | util/render_template.go
	@echo ./util/render_template.go < $< > $@
	@./util/render_template.go < $< > $@.new && mv $@.new $@ || (rm $@.new; false)

################################################################################

test: static-tests cover.html
	@printf "\e[1;32m>> All tests successful.\e[0m\n"

# which packages to test with static checkers
GO_ALLPKGS := $(shell go list ./... | grep -v '/util')
# which files to test with static checkers (this contains a list of globs)
GO_ALLFILES := $(addsuffix /*.go,$(patsubst $(shell go list .),.,$(GO_ALLPKGS)))
# which packages to test with "go test"
GO_TESTPKGS := $(shell go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./... | grep -v '/util')
# which packages to measure coverage for
GO_COVERPKGS := $(shell go list ./... | grep -Ev '/util')
# to get around weird Makefile syntax restrictions, we need variables containing a space and comma
space := $(null) $(null)
comma := ,

static-tests: FORCE
	@if ! hash golint 2>/dev/null; then printf "\e[1;36m>> Installing golint...\e[0m\n"; GO111MODULE=off go get -u golang.org/x/lint/golint; fi
	@printf "\e[1;36m>> gofmt\e[0m\n"
	@if s="$$(gofmt -s -d $(GO_ALLFILES) 2>/dev/null)" && test -n "$$s"; then echo "$$s"; false; fi
	@printf "\e[1;36m>> golint\e[0m\n"
	@if s="$$(golint $(GO_ALLPKGS) 2>/dev/null)" && test -n "$$s"; then echo "$$s"; false; fi
	@printf "\e[1;36m>> go vet\e[0m\n"
	@go vet $(GO_BUILDFLAGS) $(GO_ALLPKGS)

cover.out: FORCE
	@printf "\e[1;36m>> go test\e[0m\n"
	@env $(GO_TESTENV) go test $(GO_BUILDFLAGS) -ldflags '-s -w $(GO_LDFLAGS)' -p 1 -coverprofile=$@ -covermode=count -coverpkg=$(subst $(space),$(comma),$(GO_COVERPKGS)) $(GO_TESTPKGS)

cover.html: cover.out
	@printf "\e[1;36m>> go tool cover > $@\e[0m\n"
	@go tool cover -html=$< -o $@

################################################################################

.PHONY: FORCE
