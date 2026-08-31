#!/usr/bin/env sh
set -eu

script_dir=$(cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(cd -- "$script_dir/.." && pwd)

for required_dir in "$repo_root/cmd" "$repo_root/internal"; do
	if [ ! -d "$required_dir" ]; then
		printf 'required directory is missing: %s\n' "$required_dir" >&2
		exit 2
	fi
done

if ! empty_dir=$(find "$repo_root/cmd" "$repo_root/internal" -type d -empty -print -quit); then
	printf 'failed to inspect repository directories\n' >&2
	exit 2
fi

if [ -n "$empty_dir" ]; then
	printf 'empty directory: %s\n' "$empty_dir" >&2
	exit 1
fi

printf '%s\n' 'no empty directories under cmd or internal'
