help:
	@echo 'Available targets:'
	@echo '    make generate'
	@echo '    make test'

################################################################################

generate: generated.go

%: %.in | util/render_template.go
	@echo ./util/render_template.go < $< > $@
	@./util/render_template.go < $< > $@.new && mv $@.new $@ || (rm $@.new; false)

################################################################################

test: static-tests cover.html

PKG = github.com/majewsky/schwift
TESTPKGS = $(PKG) $(PKG)/tests          # space-separated list of packages containing tests
COVERPKGS = $(PKG),$(PKG)/gopherschwift # comma-separated list of packages for which to measure coverage

static-tests: FORCE
	@echo '>> gofmt...'
	@if s="$$(gofmt -s -l $$(find . -name \*.go) 2>/dev/null)" && test -n "$$s"; then echo "$$s"; false; fi
	@echo '>> golint...'
	@if s="$$(golint $(TESTPKGS) 2>/dev/null)" && test -n "$$s"; then echo "$$s"; false; fi
	@echo '>> govet...'
	@go vet $(TESTPKGS)

cover.out.%: FORCE
	@echo '>> go test...'
	go test -covermode count -coverpkg $(COVERPKGS) -coverprofile $@ $(subst _,/,$*)
cover.out: $(addprefix cover.out.,$(subst /,_,$(TESTPKGS)))
	util/gocovcat.go $^ > $@
cover.html: cover.out
	@echo '>> rendering cover.html...'
	@go tool cover -html=$< -o $@

################################################################################

# vendoring by https://github.com/holocm/golangvend
vendor: FORCE
	@golangvend

.PHONY: FORCE
