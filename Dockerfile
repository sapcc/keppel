FROM golang:1.17.11-alpine3.16 as builder
RUN apk add --no-cache gcc git make musl-dev

COPY . /src
RUN make -C /src install PREFIX=/pkg GO_BUILDFLAGS='-mod vendor'

################################################################################

FROM alpine:3.16

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /pkg/ /usr/

ARG COMMIT_ID=unknown
LABEL source_repository="https://github.com/sapcc/keppel" \
  org.opencontainers.image.url="https://github.com/sapcc/keppel" \
  org.opencontainers.image.revision=${COMMIT_ID}

USER nobody:nobody
WORKDIR /var/empty
ENTRYPOINT [ "/usr/bin/keppel" ]
