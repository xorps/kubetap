# Kubetap Development

This section provides information about Kubetap development processes and how
to contribute.

## Kubetap overview

Kubtap is developed by [Matt Hamilton](https://github.com/eriner) at [Soluble.ai](https://www.soluble.ai/) as an [Apache v2 Licensed](https://github.com/xorps/kubetap/blob/master/LICENSE)
open source project.

Kubetap arose from a need to quickly and efficiently proxy Kubernetes Services
without imposing a CNI mandate.

Building kubetap requires the following dependencies:

| dependency | purpose               | notes                                         |
| ---        | ---                   | ---                                           |
| `kubectl`  | ...                   | mandatory for integration tests               |
| `docker`   | Build containers      | not needed to build `kubectl-tap` binary      |
| `go`       | Build `kubectl-tap`   | see `go.mod` for the required Go version      |
| `zsh`      | Build scripts         | scripting is nicer than `bash` or `sh`        |

Script-managed dependencies, run via `go run`/`go install` at pinned versions:

| dependency      | purpose | notes                                           |
| ---             | ---     | ---                                             |
| `golangci-lint` | Linting | used as Go code linter                          |
| `gotestsum`     | Testing | used to make test output prettier               |
| `kind`          | Testing | required for integration tests                  |

## Hacking on Kubetap

Assuming you have [built kubetap from source](../getting_started/installation.md),
you're ready to hack on kubetap.

```sh
$ cd ${GOPATH}/src/github.com/xorps/kubetap

$ go generate .

$ go build ./cmd/kubectl-tap
```
