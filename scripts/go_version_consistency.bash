#!/bin/bash
set -euo pipefail

go_major_minor="$(go mod edit -json | jq -r .Go | awk -F. '{print $1"."$2}')"

expect_alpine="golang:${go_major_minor}-alpine"
expect_gae="runtime_version: \"${go_major_minor}\""

exit_code=0
if ! grep -qF "$expect_alpine" Dockerfile; then
	echo "ERROR: Dockerfile is not using '$expect_alpine'; please fix it."
	exit_code=1
fi

if ! grep -qF "$expect_gae" app.yaml; then
	echo "ERROR: app.yaml is not using '$expect_gae'; please fix it."
	exit_code=1
fi

if ! grep -qF "$expect_alpine" cloudbuild.yaml; then
	echo "ERROR: cloudbuild.yaml is not using '$expect_alpine'; please fix it."
	exit_code=1
fi

exit "${exit_code}"
