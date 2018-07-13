#!/bin/sh
awk '$1 == "#" { if (/TBD/) { print $2"-dev" } else { print $2 } }' CHANGELOG.md | sed 's/^v//' | head -n1
