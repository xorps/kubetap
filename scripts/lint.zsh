#!/usr/bin/env zsh

script_dir=${0:A:h}
source ${script_dir}/_pre.zsh

GOLANGCI_LINT_VERSION=v2.15.0

# fetch dependencies
go mod download

# install the pinned golangci-lint binary if it's missing or a different version
# See: https://golangci-lint.run/docs/welcome/install/local/
if [[ "$(golangci-lint version --short 2>/dev/null)" != "${GOLANGCI_LINT_VERSION#v}" ]]; then
  curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$(go env GOPATH)/bin" ${GOLANGCI_LINT_VERSION}
fi

# run golangci-lint
"$(go env GOPATH)/bin/golangci-lint" run

source ${script_dir}/_post.zsh
