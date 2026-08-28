#!/bin/sh
set -eu

CDPATH=''
export CDPATH
repository_root=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repository_root"

version=$(tr -d '\r\n' < VERSION)
if ! printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$'; then
	printf 'VERSION must contain one semantic version without a leading v\n' >&2
	exit 1
fi

if ! grep -Fq "## $version - " CHANGELOG.md; then
	printf 'CHANGELOG.md must contain a dated ## %s entry\n' "$version" >&2
	exit 1
fi

notes="docs/releases/v${version}.md"
if [ ! -f "$notes" ]; then
	printf 'missing release notes: %s\n' "$notes" >&2
	exit 1
fi

printf '%s\n' "$version"
