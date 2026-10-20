#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

GOLANGCI_LINT_VERSION=v2.15.0

# fetch dependencies
go mod download

# run golangci-lint without adding it to go.mod
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} run

source ${script_dir}/_post.zsh
