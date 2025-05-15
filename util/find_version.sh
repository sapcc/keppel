#!/bin/sh

# SPDX-FileCopyrightText: 2025 SAP SE
#
# SPDX-License-Identifier: Apache-2.0

awk '$1 == "#" { if (/TBD/) { print $2"-dev" } else { print $2 } }' CHANGELOG.md | sed 's/^v//' | head -n1
