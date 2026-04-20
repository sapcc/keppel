module github.com/sapcc/keppel

go 1.26

require (
	github.com/alicebob/miniredis/v2 v2.37.0
	github.com/databus23/goslo.policy v0.0.0-20250326134918-4afc2c56a903
	github.com/dlmiddlecote/sqlstats v1.0.2
	github.com/go-gorp/gorp/v3 v3.1.0
	github.com/go-redis/redis_rate/v10 v10.0.1
	github.com/gofrs/uuid/v5 v5.4.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/cel-go v0.28.0
	github.com/gophercloud/gophercloud/v2 v2.12.0
	github.com/gorilla/mux v1.8.1
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/majewsky/gg v1.6.0
	github.com/opencontainers/distribution-spec/specs-go v0.0.0-20260413194130-13a5d3ee2857
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.1.1
	github.com/prometheus/client_golang v1.23.2
	github.com/redis/go-redis/v9 v9.18.0
	github.com/rs/cors v1.11.1
	github.com/sapcc/go-api-declarations v1.21.0
	github.com/sapcc/go-bits v0.0.0-20260420121059-0f152034c842
	github.com/spf13/cobra v1.10.2
	github.com/timewasted/go-accept-headers v0.0.0-20130320203746-c78f304b1b09
	go.podman.io/image/v5 v5.39.2
	go.xyrillian.de/schwift/v2 v2.1.0
)

require (
	cel.dev/expr v0.25.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containers/libtrust v0.0.0-20230121012942-c1716e8a8d01 // indirect
	github.com/containers/ocicrypt v1.2.1 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/docker/docker v28.5.1+incompatible // indirect
	github.com/golang-migrate/migrate/v4 v4.19.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/itchyny/gojq v0.12.19 // indirect
	github.com/itchyny/timefmt-go v0.1.8 // indirect
	github.com/jpillora/longestcommon v0.0.0-20161227235612-adb9d91ee629 // indirect
	github.com/lib/pq v1.12.3 // indirect
	github.com/moby/moby/client v0.1.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.5 // indirect
	github.com/prometheus/procfs v0.17.0 // indirect
	github.com/rabbitmq/amqp091-go v1.10.0 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.podman.io/storage v1.62.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/exp v0.0.0-20240823005443-9b4947da3948 // indirect
	golang.org/x/sys v0.43.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250825161204-c5933d9347a5 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250825161204-c5933d9347a5 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/go-gorp/gorp/v3 v3.1.0 => github.com/majewsky/gorp/v3 v3.1.1-0.20260409143009-8eddaa758ac1

// TODO: remove with 5.39.0 release
replace go.podman.io/image/v5 => go.podman.io/image/v5 v5.38.1-0.20251112190108-acb3639700ff
