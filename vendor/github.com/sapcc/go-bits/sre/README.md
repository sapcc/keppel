# sre

Package sre contains a HTTP middleware that emits SRE-related Prometheus metrics.

It's similar to [github.com/prometheus/client\_golang/prometheus/promhttp][promhttp], but with an
additional "endpoint" label identifying the type of request. The final request handler must identify
itself to this middleware by calling IdentifyEndpoint().

[promhttp]: https://godoc.org/github.com/prometheus/client_golang/prometheus/promhttp
