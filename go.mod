module github.com/sapcc/keppel

go 1.12

require (
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/libtrust v0.0.0-20160708172513-aabc10ec26b7
	github.com/go-gorp/gorp v2.2.0+incompatible // indirect
	github.com/go-sql-driver/mysql v1.5.0 // indirect
	github.com/gophercloud/gophercloud v0.6.0
	github.com/gophercloud/utils v0.0.0-20191115025210-6e51b8944d05
	github.com/gorilla/mux v1.7.3
	github.com/jarcoal/httpmock v1.0.4
	github.com/jpillora/longestcommon v0.0.0-20161227235612-adb9d91ee629 // indirect
	github.com/majewsky/schwift v0.0.0-20180906125654-e1b3d5e2efc9
	github.com/mattn/go-sqlite3 v2.0.3+incompatible // indirect
	github.com/opencontainers/go-digest v1.0.0-rc1
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/poy/onpar v0.0.0-20190519213022-ee068f8ea4d1 // indirect
	github.com/prometheus/client_golang v1.2.1
	github.com/rs/cors v1.7.0
	github.com/sapcc/go-bits v0.0.0-20200202190900-c4dbd089c539
	github.com/sapcc/hermes v0.0.0-20191022102637-1c86dbdcbd15
	github.com/satori/go.uuid v1.2.0
	github.com/ziutek/mymysql v1.5.4 // indirect
	golang.org/x/crypto v0.0.0-20190308221718-c2843e01d9a2
	gopkg.in/gorp.v2 v2.2.0
)

replace rsc.io/letsencrypt => github.com/sapcc/letsencrypt-go-shim v0.0.3
