# Auth driver: `keystone`

An auth driver using the Keystone V3 API of an OpenStack cluster. With this driver, Keppel auth tenants correspond to
Keystone projects.

- Requests to the [Keppel API](../api-spec.md) are authenticated by reading a Keystone token from the X-Auth-Token
  request header.
- Requests to the Docker Registry API are authenticated with username and password, and the username has one of the
  following formats:
  ```
  user_name@user_domain_name/project_name@project_domain_name
  user_name@domain_name/project_name
  ```
  The latter format implies that user and project are located in the same domain.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `OS_...` | *(required)* | A full set of OpenStack auth environment variables for Keppel's service user. See [documentation for openstackclient][os-env] for details. |
| `KEPPEL_AUTH_LOCAL_ROLE` | *(required)* | A Keystone role name that will be assigned to Keppel's service user in a project when creating a Keppel account there, in order to enable access to this project for the storage driver. For the `swift` storage driver, this will usually be `swiftoperator`. |
| `KEPPEL_OSLO_POLICY_PATH` | *(required)* | Path to the `policy.json` file for this service. |

Keppel understands access rules in the [`oslo.policy` JSON format][os-pol]. An example can be seen at
[`docs/example-policy.json`](../example-policy.json). The following rules are expected:

- `account:list` is required for any non-anonymous access to the API.
- `account:show` enables read access to repository and tag listings.
- `account:pull` allows to `docker pull` images.
- `account:push` allows to `docker push` images.
- `account:edit` enables write access to an account's configuration.

[os-env]: https://docs.openstack.org/python-openstackclient/latest/cli/man/openstack.html
[os-pol]: https://docs.openstack.org/oslo.policy/latest/admin/policy-json-file.html
