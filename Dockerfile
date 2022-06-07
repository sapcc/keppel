# renovate: datasource=docker depName=alpine versioning=docker
ARG ALPINE_VERSION=3.16
# renovate: datasource=docker depName=golang versioning=docker
ARG GOLANG_VERSION=1.17.11-alpine

FROM golang:${GOLANG_VERSION}${ALPINE_VERSION} as builder
RUN apk add --no-cache gcc git make musl-dev

COPY . /src
RUN make -C /src install PREFIX=/pkg GO_BUILDFLAGS='-mod vendor'

################################################################################

FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates
COPY --from=builder /pkg/ /usr/

LABEL source_repository="https://github.com/sapcc/keppel" \
  org.opencontainers.image.url="https://github.com/sapcc/keppel" \
  org.opencontainers.image.revision=unknown

USER nobody:nobody
WORKDIR /var/empty
ENTRYPOINT [ "/usr/bin/keppel" ]
