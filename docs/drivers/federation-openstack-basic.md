<!--
SPDX-FileCopyrightText: 2025 SAP SE

SPDX-License-Identifier: Apache-2.0
-->

# Federation driver: `openstack-basic`

A simple federation driver for use with the [`keystone` auth driver](./auth-keystone.md). Claims are checked against a
hardcoded whitelist. This driver is OpenStack-specific since it translates auth tenant IDs (i.e., project IDs) into
project names before checking.

The whitelist looks like this:

```
project1:accountName1,project2:accountName2,project3:accountName3,...
```

Herein, each `project1` etc. is a regex matching Keystone project names (in the form `projectName@domainName`), and each
`accountName1` etc. is a regex matching account names. A leading `^` and trailing `$` are automatically added to each
regex. For example, the whitelist entry `foo.*@bar:qux.*` will allow all projects in the domain `bar` whose name starts
with `foo` to claim account names starting with `qux`.

The whitelist may end with a trailing comma to make templating easier.

| Variable | Default | Explanation |
| -------- | ------- | ----------- |
| `KEPPEL_NAMECLAIM_WHITELIST` | *(required)* | A whitelist, as explained above. |
