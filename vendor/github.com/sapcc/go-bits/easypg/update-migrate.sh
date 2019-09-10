#!/bin/sh
set -xeuo pipefail

################################################################################
# Refer to README.md for what's going on in here.
################################################################################

cd "$(dirname "$0")/vendoring-helper"

# force usage of go.mod (not relevant for Go 1.13+)
export GO111MODULE=on

# ensure that vendoring-helper compiles and that its go.sum is up-to-date
go build -o /dev/null .

# pull github.com/golang-migrate/migrate and its deps
go mod vendor

# move the desired code up into the easypg source tree
rm -rf -- ../migrate/
cp -R vendor/github.com/golang-migrate/migrate/v4/ ../migrate/

# filter out documentation and other stuff that we don't need
find ../migrate/ -type f ! \( -name \*.go -o -name LICENSE\* \) -delete
find ../migrate/ -type f -name \*_test.go -delete

# redirect references to github.com/golang-migrate/migrate to our own excerpt
find ../migrate/ -type f -exec sed -i 's+"github.com/golang-migrate/migrate/v4+"github.com/sapcc/go-bits/easypg/migrate+' {} +

# verify that easypg still builds
cd ..
go build -o /dev/null .
