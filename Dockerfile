FROM golang:1.10-alpine as builder
WORKDIR /x/src/github.com/sapcc/keppel/
RUN apk add --no-cache make

COPY . .
RUN make install PREFIX=/pkg

################################################################################

FROM alpine:latest
MAINTAINER "Stefan Majewsky <stefan.majewsky@sap.com>"

COPY --from=builder /pkg/ /usr/
