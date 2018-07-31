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
```

The format for libpq connection URLs is described in [this section of the PostgreSQL docs](https://www.postgresql.org/docs/9.6/static/libpq-connect.html#LIBPQ-CONNSTRING).

The `openstack.user_id` field is stupid and we're aware. It will become obsolete when [this upstream issue](https://github.com/gophercloud/gophercloud/issues/1141) has been accepted.
