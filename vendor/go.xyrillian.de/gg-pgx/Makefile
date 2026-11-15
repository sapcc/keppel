# SPDX-FileCopyrightText: 2025 Stefan Majewsky <majewsky@gmx.net>
# SPDX-License-Identifier: Apache-2.0

default: help

check: static-check build/cover.html

static-check: FORCE
	@printf "\e[1;36m>> golangci-lint\e[0m\n"
	@golangci-lint config verify
	@golangci-lint run
	@printf "\e[1;36m>> reuse lint\e[0m\n"
	@if ! reuse lint -q; then reuse lint; fi

GO_COVERPKGS := $(shell go list ./... | tr '\n' , | sed 's/,$$//')
GO_TESTPKGS := $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)

build/cover.out: FORCE
	@printf "\e[1;36m>> go test\e[0m\n"
	go test -shuffle=on -coverprofile=build/cover.out -covermode=count -coverpkg=$(GO_COVERPKGS) $(GO_TESTPKGS)
build/cover.html: build/cover.out
	@printf "\e[1;36m>> go tool cover\e[0m\n"
	go tool cover -html $< -o $@

help: FORCE
	@printf "\n"
	@printf "\e[1mUsage:\e[0m\n"
	@printf "  make \e[36m<target>\e[0m\n"
	@printf "\n"
	@printf "\e[1mTest\e[0m\n"
	@printf "  \e[36mcheck\e[0m                        Run all tests and checks.\n"
	@printf "  \e[36mstatic-check\e[0m                 Run static code checks.\n"
	@printf "  \e[36mbuild/cover.out\e[0m              Run tests and generate coverage report.\n"
	@printf "  \e[36mbuild/cover.html\e[0m             Generate an HTML file with source code annotations from the coverage report.\n"
	@printf "\n"

.PHONY: FORCE
