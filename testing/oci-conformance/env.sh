export KEPPEL_API_PUBLIC_FQDN=localhost
export KEPPEL_ISSUER_KEY=./privkey.pem
export KEPPEL_DB_CONNECTION_OPTIONS=sslmode=disable
export KEPPEL_DB_PASSWORD=mysecretpassword
export KEPPEL_DB_PORT=54321
export KEPPEL_USERNAME=johndoe
export KEPPEL_PASSWORD=SuperSecret

export KEPPEL_DRIVER_AUTH=trivial
export KEPPEL_DRIVER_FEDERATION=trivial
export KEPPEL_DRIVER_INBOUND_CACHE=trivial
export KEPPEL_DRIVER_STORAGE=in-memory-for-testing

# export KEPPEL_OSLO_POLICY_PATH=docs/example-policy.yaml
