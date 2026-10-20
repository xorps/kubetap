#!/bin/sh

GOLANGCI_LINT_VERSION=v2.15.0
GOTESTSUM_VERSION=v1.13.0
KIND_VERSION=v0.33.0

if ! [ -x "$(command -v kubectl)" ]; then
  echo "kubectl is not installed"
  exit 1
fi

# golangci-lint recommends its prebuilt binaries over `go install`
# See: https://golangci-lint.run/docs/welcome/install/local/
if ! [ -x "$(command -v golangci-lint)" ]; then
  curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$(go env GOPATH)/bin" ${GOLANGCI_LINT_VERSION}
fi

if ! [ -x "$(command -v gotestsum)" ]; then
  go install gotest.tools/gotestsum@${GOTESTSUM_VERSION}
fi

if ! [ -x "$(command -v kind)" ]; then
  go install sigs.k8s.io/kind@${KIND_VERSION}
fi
