# renovate: datasource=docker depName=alpine versioning=docker
ARG ALPINE_VERSION=3.15
# renovate: datasource=docker depName=golang versioning=docker
ARG GOLANG_VERSION=1.18.1-alpine

FROM golang:${GOLANG_VERSION}${ALPINE_VERSION} as builder
RUN apk add --no-cache make gcc musl-dev

COPY . /src
RUN make -C /src install PREFIX=/pkg GO_BUILDFLAGS='-mod vendor'

################################################################################

FROM alpine:${ALPINE_VERSION}
LABEL source_repository="https://github.com/sapcc/keppel"

RUN apk add --no-cache ca-certificates
COPY --from=builder /pkg/ /usr/
ENTRYPOINT [ "/usr/bin/keppel" ]
