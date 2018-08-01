#!/bin/sh
set -euo pipefail

export TEMP_PATH="$(mktemp -d)"
cleanup() {
  rm -rf -- "${TEMP_PATH}"
}
trap cleanup EXIT

openssl genrsa -out "${TEMP_PATH}/privkey.pem" 4096 2>/dev/null
openssl req -x509 -sha256 -days 365 -subj "/CN=keppel" \
  -key "${TEMP_PATH}/privkey.pem" -out "${TEMP_PATH}/cert.pem"

echo 'trust:'
echo '  issuer_key: |'
sed 's/^/    /' "${TEMP_PATH}/privkey.pem"
echo '  issuer_cert: |'
sed 's/^/    /' "${TEMP_PATH}/cert.pem"
