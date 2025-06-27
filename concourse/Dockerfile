FROM keppel.global.cloud.sap/ccloud-dockerhub-mirror/library/alpine:3

RUN apk add --no-cache --no-progress bash curl jq

LABEL source_repository=https://github.com/sapcc/keppel/

COPY ["in", "out", "check", "/opt/resource/"]
