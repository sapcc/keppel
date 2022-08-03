FROM golang:1.18.4-alpine3.16 as builder

RUN apk add --no-cache gcc git make musl-dev

COPY . /src
ARG BININFO_BUILD_DATE BININFO_COMMIT_HASH BININFO_VERSION # provided to 'make install'
RUN make -C /src install PREFIX=/pkg GO_BUILDFLAGS='-mod vendor'

################################################################################

FROM alpine:3.16

RUN apk add --no-cache ca-certificates
COPY --from=builder /pkg/ /usr/

ARG BININFO_BUILD_DATE BININFO_COMMIT_HASH BININFO_VERSION
LABEL source_repository="https://github.com/sapcc/keppel" \
  org.opencontainers.image.url="https://github.com/sapcc/keppel" \
  org.opencontainers.image.created=${BININFO_BUILD_DATE} \
  org.opencontainers.image.revision=${BININFO_COMMIT_HASH} \
  org.opencontainers.image.version=${BININFO_VERSION}

USER nobody:nobody
WORKDIR /var/empty
ENTRYPOINT [ "/usr/bin/keppel" ]
