module github.com/sapcc/keppel

go 1.12

require (
	github.com/Shopify/logrus-bugsnag v0.0.0-20171204204709-577dee27f20d // indirect
	github.com/aryann/difflib v0.0.0-20170710044230-e206f873d14a
	github.com/bshuster-repo/logrus-logstash-hook v0.4.1 // indirect
	github.com/bugsnag/bugsnag-go v1.5.3 // indirect
	github.com/bugsnag/panicwrap v1.2.0 // indirect
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/go-metrics v0.0.1 // indirect
	github.com/docker/libtrust v0.0.0-20160708172513-aabc10ec26b7
	github.com/garyburd/redigo v1.6.0 // indirect
	github.com/gophercloud/gophercloud v0.6.0
	github.com/gophercloud/utils v0.0.0-20191115025210-6e51b8944d05
	github.com/gorilla/handlers v1.4.2 // indirect
	github.com/gorilla/mux v1.7.3
	github.com/jarcoal/httpmock v1.0.4
	github.com/jpillora/longestcommon v0.0.0-20161227235612-adb9d91ee629 // indirect
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0 // indirect
	github.com/lib/pq v1.2.0
	github.com/majewsky/schwift v0.0.0-20180906125654-e1b3d5e2efc9
	github.com/mattes/migrate v3.0.1+incompatible
	github.com/mitchellh/mapstructure v1.1.2
	github.com/opencontainers/go-digest v1.0.0-rc1
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/prometheus/client_golang v1.2.1
	github.com/rs/cors v1.7.0
	github.com/sapcc/go-bits v0.0.0-20191125140305-f4a863758644
	github.com/sapcc/hermes v0.0.0-20191022102637-1c86dbdcbd15
	github.com/satori/go.uuid v1.2.0
	github.com/spf13/cobra v0.0.5 // indirect
	github.com/yvasiyarov/go-metrics v0.0.0-20150112132944-c25f46c4b940 // indirect
	github.com/yvasiyarov/gorelic v0.0.7 // indirect
	github.com/yvasiyarov/newrelic_platform_go v0.0.0-20160601141957-9c099fbc30e9 // indirect
	golang.org/x/crypto v0.0.0-20190308221718-c2843e01d9a2
	gopkg.in/gorp.v2 v2.2.0
	k8s.io/api v0.0.0-20190620084959-7cf5895f2711
	k8s.io/apimachinery v0.0.0-20190612205821-1799e75a0719
	k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
	rsc.io/letsencrypt v0.0.0-00010101000000-000000000000 // indirect
)

replace rsc.io/letsencrypt => github.com/sapcc/letsencrypt-go-shim v0.0.3
