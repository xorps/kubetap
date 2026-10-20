#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

GOTESTSUM_VERSION=v1.13.0

# test, using gotestsum to pretty-format the output
go run gotest.tools/gotestsum@${GOTESTSUM_VERSION} --format=short-verbose --no-summary=skipped --junitfile=coverage.xml -- -count=1 -race -coverprofile=coverage.txt -covermode=atomic ./...

# ensure we can build
go build -v -trimpath -ldflags="-s -w" -o /dev/null ./cmd/kubectl-tap

# tidy modules, in case this is a local build env
go mod tidy

source ${script_dir}/_post.zsh
