FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
RUN apk add --no-cache upx
RUN --mount=type=bind,target=. \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-w -s" -o /go/bin/kubectl-tap ./cmd/kubectl-tap && \
    upx /go/bin/kubectl-tap

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS alpine
WORKDIR /usr/share/zoneinfo
RUN apk --no-cache add tzdata zip ca-certificates && \
    zip -r -0 /zoneinfo.zip .

FROM scratch
WORKDIR /app
COPY --from=build /go/bin/kubectl-tap .
ENV ZONEINFO=/zoneinfo.zip
COPY --from=alpine /zoneinfo.zip /
COPY --from=alpine /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["./kubectl-tap"]
