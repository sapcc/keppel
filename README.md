# keppel

Federated multi-tenant container image registry

## Usage

Run with

```bash
make && PATH=$PWD/build:$PATH keppel-api
```

`keppel-api` expects that `keppel-registry` exists in `$PATH`, hence the manipulation of `$PATH` in this example.
`keppel-api` requires one argument, a path to a config file like this:

```yaml
api:
  # listen address for HTTP server of keppel-api (optional, value shown is the default)
  listen_address: :8080
  # URL where users reach the keppel-api
  public_url: https://keppel.example.com

db:
  # a libpq connection URL
  url: postgres://postgres@localhost/keppel

openstack:
  auth:
    # credentials for service user (only Identity V3 is supported)
    auth_url: https://keystone.example.com/v3
    user_name: keppel
    user_domain_name: Default
    password: swordfish
    project_name: service
    project_domain_name: Default

  # a Keystone role name that enables read-write access to a project's Swift
  # account when assigned at the project level
  local_role: swiftoperator
  # path to the oslo.policy file for the keppel-api
  policy_path: /home/user/go/src/github.com/sapcc/keppel/docs/example-policy.json
  # PROVISIONAL: the user ID for the Keppel service user (as identified by
  # openstack.auth.user_name and openstack.auth.user_domain_name)
  user_id: 790b87de4ec44ed4a4270b993d62905f

trust:
  issuer_key: /var/lib/keppel/privkey.pem
  issuer_cert: /var/lib/keppel/cert.pem
```

The format for libpq connection URLs is described in [this section of the PostgreSQL docs](https://www.postgresql.org/docs/9.6/static/libpq-connect.html#LIBPQ-CONNSTRING).

The `openstack.user_id` field is stupid and we're aware. It will become obsolete when [this upstream issue](https://github.com/gophercloud/gophercloud/issues/1141) has been accepted.

The key pair in the `trust` section is used for authentication: keppel-api signs tokens for Docker clients with the
given private key, and keppel-registry verifies the tokens using the given certificate. Instead of specifying a
filename, you can also supply the key/cert directly in PEM format. The Subject Public Key of the certificate must be the
public counterpart of the private issuer key. You can generate a suitable `trust` section by running `bash
./util/generate_trust.sh` in the repo root directory. Note that certificates expire! `util/generate_trust.sh` will
generate a certificate with a validity of 1 year.
