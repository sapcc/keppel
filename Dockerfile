# renovate: datasource=docker depName=alpine versioning=docker
ARG ALPINE_VERSION=3.13

FROM golang:1.17-alpine${ALPINE_VERSION} as builder
RUN apk add --no-cache make gcc musl-dev

COPY . /src
RUN make -C /src install PREFIX=/pkg GO_BUILDFLAGS='-mod vendor'

################################################################################

FROM alpine:${ALPINE_VERSION}
LABEL source_repository="https://github.com/sapcc/keppel"

RUN apk add --no-cache ca-certificates
COPY --from=builder /pkg/ /usr/
ENTRYPOINT [ "/usr/bin/keppel" ]
