PKG    = github.com/sapcc/keppel
PREFIX := /usr

GO            := GOPATH=$(CURDIR)/.gopath GOBIN=$(CURDIR)/build go
GO_BUILDFLAGS :=
GO_LDFLAGS    := -s -w

build_all: $(patsubst cmd/%/main.go,build/%,$(wildcard cmd/*/main.go))
build/%:
	$(GO) install $(GO_BUILDFLAGS) -ldflags '$(GO_LDFLAGS)' '$(PKG)/cmd/$*'
