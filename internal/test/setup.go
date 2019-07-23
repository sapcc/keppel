/*******************************************************************************
*
* Copyright 2018 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sapcc/keppel/internal/keppel"
)

//Setup parses the given Keppel configuration and sets up keppel.TestMode and
//keppel.State for the test.
func Setup(t *testing.T, configYAML string) {
	t.Helper()
	keppel.TestMode = true

	//WTF: YAML parser chokes on leading tabs
	configYAML = strings.Replace(configYAML, "\t", "  ", -1)
	configYAML += "\n    trust:"
	configYAML += "\n      issuer_key: " + fmt.Sprintf("%q", UnitTestIssuerPrivateKey)
	configYAML += "\n      issuer_cert: " + fmt.Sprintf("%q", UnitTestIssuerCert)

	err := keppel.ReadConfig(strings.NewReader(configYAML))
	if err != nil {
		t.Fatal(err.Error())
	}
}

//UnitTestIssuerPrivateKey is an RSA private key that can be used as
//trust.issuer_key in unit tests. DO NOT USE IN PRODUCTION.
var UnitTestIssuerPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
MIIJKgIBAAKCAgEApaFTmtIHzEg9dznhoFgOKqZseh4PcXTITEc0F/1Gjj/zQmKj
0jOlbTQv/4IbmFPVP75dGB+Dw5qHh+4TR8uObx6VudnkSHrn8buPKD1n2T5r/SMY
2mHATL40Tu+5RVmBCJfYTNhjYhOVc5si06CTIYhjBTitWsJcTiG0zcYYySizhqGg
bBF8faO24BoL4n0O8H6+J8WIyOxlkGbaKJqDaiagazqX4Ii4PTe2AmlT/CHVnU6s
j3FM9OI5ksoF4RPyBIkaAZGFu7iHXKmZS46AkrNOwXrYadLG0lQuhY9CdqMzixIv
NViIkSIfOjxLhqioyVMKarYWQFwb6HNfAQAa56Z+gvWImgFAw5yRbtb0yuK8N+nP
dWhLPQw6JnYhlHrZJ1+108fkFlgbGCUSOgPvs2XO2B2fd8QWisXhQCahariuYqPj
3oGnu224sLaTLDR177NGmZqwOR038/7cOE3VJTFdAWTmdGmkz3B8DcsAvzishKSo
yi1bWytIKNrrwXPDR9wxuATHsstiZXlEixyD5rJLP+RxkCocTx5Wg9S2KkoUP/zM
QMw0aOOrk/7rqlM9w2ZkACuTkioC5ynw5Yco7VHdmkzm4nEnuHj9gOAalRl8kJ0r
X7ozarcZEMn3hkDL1F+SYdBYx2unf4od2r/fxXTYeaVVwjah1PQXs+Tg+/8CAwEA
AQKCAgEAgvk/k33ijLfTYyRyNslq6m8P+MEslRs0CJ2FpDK0SGhphGVcBiyw89oA
2puYFqy0ROPT2e+R0muwIN0ygeOFjnkxDPYwfuAx6gXW/osQQ8oIuvO2A3qpBgai
dok6iIxubM0mTh4O+M9jrzdOIusnbazcIJThAJQRSfd9cfrkPq3gyOWmZc6uEuwT
AMOYAlHCLosK84hQ0hGdfsLWYKVOpfJFiIWc9AEpL7+OPfnsX8ShlvNPoV6G7F64
CEuYupN7HfsMhZD9n6Qb5jp27jiRk3AXJwhtecEjV88ZuqO+evIzIBYRHq4T0DCb
YQGs958HWaxA4IF8twgfSYFx7uiWXLH0jcJgDLxb/JAkQs276+2ZIm0gq8+k4Pzi
an1weYH/5n4UWVo+Oqfe/+D5+yU3k5mGC7uk7oPEncCLvJRFK05XxEaMuTek5VI3
kuX8o9V/pHmz1sFEC3H1bO2wadU8gMmP3lMyUxE9p/h64fJ1nWTn/PI3lZP++IZQ
idp8iBGpB6YSDNaesj9KZlUoJg43KIm1p0kENE+CBsQgtFA446ot7u+umZiHP5AM
tkYJ3apTS6ORtc0X+0k+ZhcKORKDBnlKl9uxDQqAlsKYvZGaNDJZlRFgOrKAs4q3
yNYO6v9kcxqd0BJ6hkh9w++bQtyCTUgjx+EjfgDnsqa+SmDHDoECggEBANxD/Ege
7gcSYoppXj26BjhyXymRUWoK37Ao+sn+sIqr8pxv+wRBGcPTeFFcpvegXqlUuAow
7IThpS+9i49hacKb9pXuJ8nfHNlfDtcxW4HOQzZajq/tp4pdBOaZRznO3tDbD/u8
ubJHQOUWIVakOx53xuHcS40CNNNivVj380ykX3LW7i+DDD26HYcfHcXobtKZXVGi
Cc8aA7EdcZeWSnDHjlNmUC8cAAbB8CeBiqHZ/2kOy/Ef0lnYI/8XmjjObY9u/18y
XOlSP7I2tAd6lZqnvzPQAaI9QZG9XuC236s8GFSk1zuT9yu6xEHs0A6BwnEntYVZ
18D59EVFdY/fnesCggEBAMCAPObiIM+yAqQiH15afkaE4xLmXZwBXolWm4KWfJ/8
orZ+jvNqm/dYGs7rGfe/NBawegLo6/HkqsH1PvvvJrky0HCDgb3g+k5WwwBDfuJg
QRwj1x9sl8mz0PdlNN0kR3Qa/4sRCfj2Xk1C2961He9XbeInbWsOFQIYtsZyF+cs
sXxGimcc7iurGTzDrquV5v7D5ogpuA0gYGuGuQBwKBLAW9bvfz1gsDJy5UUyJcIP
zJIX00GTj0dOfYJXRzZYeo+vaN4DCn4LhtRLWA7OSPAF3PnPVXcQxCjb4AAOTJHZ
dqct0w5u23VBKRO3E21y/LgMDa8QO4eRppk9VS2jUT0CggEAJ1DzTSRINHbxo+ce
7UGxLo4rsk3ADH+YYedOrJOLi5UZnxbV5XKBWNT8WvmAzB6SBwOaPidxcF6ej6Dz
skofCJ+yKhzyeTQcACjZi0vCG69ni+IqKfjvuODVqRue/RCR8RHJDpQnSU0ypjGH
DeIOs2eJ1nLuAWNtbnXnemP3x6xnZSY8KbroinQYJTBGrjbI4UqCv7l+qrroActR
pU8sRmk4XGac1WvYDVy8szCKQE2bK3N6r7WQZH0SH8xkuNMP91RGvQVOVE9cE0F0
bQlSfuKGXIc6Y20vsQXuU4oQ7o2xghpSWM4WhnW15laQ5KYAwRXnbsAUpNt44Ix/
aYjutQKCAQEAh1pj+C/txDw1UTVQ+yYD/g+4HnTuQyBPWaAVDlhD3rZjrpAEcbF3
Yw6HIxD6HFJMDNwfnmYqaNZRHroThE+e2b+aAlLlah6DwYuN52SOFhx6C5BD1auk
esW93AZEim3U9BV7s0vSyERrAEZPlSOincTK1abFb+3h5ax878IPfpPVZD2xWVll
Oj0/LJOnAK0RU/do5Dr5V/l48oIzGNTDyJOKv/F8dSrEGWTiQqpFFFPJkru/5i8c
IpZU983om5TQ8LD0uo5G1WPDdQhZLWfsryBgRSJ8xJB8bQJVWZS0UCUpIdm9ujtG
ggbEHEGxHlcozTxkbsCqKuPF0Z/ngYSBPQKCAQEAq81qc7tCo1mkri6oGx93hXCn
16fvn3I2a0N5G+oSECLiwixduW0BSgf04p86Ij4ga/6gVo6p/yWaj0r8mAsrmSYl
F4stF97qKZqDaSuKkDqNRszZMfHUsIPFvsX/JLW/p8+MGpzIde6i8ZDf5s8gdfxO
FvFvd6cxBsJtVH7HMLsPiYqRmMEam0C5rZEEPkUJ1L4agEU1vfV+dhCaTxus+tPe
cVD23BmXI3LgZ/sLRdZO4js/jT7C5FV9zBKooLnWn+UdMJNft3HHj4axeJZmBU17
V/EtRMqfEOel+lTJXmLb0z7YOgfPmAT2ojk86CsjwbaWwn2rlNVmu+oB8CuSAg==
-----END RSA PRIVATE KEY-----`

//UnitTestIssuerCert is a certificate that can be used as
//trust.issuer_cert in unit tests. DO NOT USE IN PRODUCTION.
var UnitTestIssuerCert = `-----BEGIN CERTIFICATE-----
MIIE+jCCAuKgAwIBAgIJAO+EjlXwlQA0MA0GCSqGSIb3DQEBCwUAMBExDzANBgNV
BAMMBmtlcHBlbDAgFw0xODA4MjcxMzI4MjlaGA8yODQwMDExMDEzMjgyOVowETEP
MA0GA1UEAwwGa2VwcGVsMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA
paFTmtIHzEg9dznhoFgOKqZseh4PcXTITEc0F/1Gjj/zQmKj0jOlbTQv/4IbmFPV
P75dGB+Dw5qHh+4TR8uObx6VudnkSHrn8buPKD1n2T5r/SMY2mHATL40Tu+5RVmB
CJfYTNhjYhOVc5si06CTIYhjBTitWsJcTiG0zcYYySizhqGgbBF8faO24BoL4n0O
8H6+J8WIyOxlkGbaKJqDaiagazqX4Ii4PTe2AmlT/CHVnU6sj3FM9OI5ksoF4RPy
BIkaAZGFu7iHXKmZS46AkrNOwXrYadLG0lQuhY9CdqMzixIvNViIkSIfOjxLhqio
yVMKarYWQFwb6HNfAQAa56Z+gvWImgFAw5yRbtb0yuK8N+nPdWhLPQw6JnYhlHrZ
J1+108fkFlgbGCUSOgPvs2XO2B2fd8QWisXhQCahariuYqPj3oGnu224sLaTLDR1
77NGmZqwOR038/7cOE3VJTFdAWTmdGmkz3B8DcsAvzishKSoyi1bWytIKNrrwXPD
R9wxuATHsstiZXlEixyD5rJLP+RxkCocTx5Wg9S2KkoUP/zMQMw0aOOrk/7rqlM9
w2ZkACuTkioC5ynw5Yco7VHdmkzm4nEnuHj9gOAalRl8kJ0rX7ozarcZEMn3hkDL
1F+SYdBYx2unf4od2r/fxXTYeaVVwjah1PQXs+Tg+/8CAwEAAaNTMFEwHQYDVR0O
BBYEFHUO/BsOlllOktauJkiBDmYkapCvMB8GA1UdIwQYMBaAFHUO/BsOlllOktau
JkiBDmYkapCvMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggIBADmG
boKW3tVbt7Fa944MZVM9vZcEUjvVWxJg4vmlEBJTStiEEg1M7dGZHjZqBvwXwc3g
rQ8z6ZBLRV/Wr0TntUZzMn75pYp52mGyPFxVd0inAhGrlD0oL7SR3JKZ8CBuJSLr
iAhkmXDvnZP1IOLWQV84r2kQ5cIWvk5G2qnB6CFgKXKbb3i/4V+x/1w22PmoRUAF
A8gE/5wQM1sWVhV8Pa1erG5N1wR0y24BiXOmfdSbVnJCj7oRyeT239TCdc0STmAr
aKFvsWyvYJunEW9Zp274cZzlp66uYJz/D2knhShqxoS7ehMYs3BymvyUL9uVIEtT
XmY0i2/1ZbxT+BQ1NOfY8F7+FvGfLEn/BsX5Cdc0sQ/B47XIkY/S/q73VZy5StWA
pqNrkrAcLsBpHoNIGT7dKStI8fyqdWvMoYj+2GNUWLJ8o5icsEK5b1W1MB8Yx1v1
k8IkGPSyDr30UOg+Hf5KHbue+mnwT+yatzRqlA0NgnHOHa/y0Lis28oA7R+mpkOz
ZqXk0KKlfG5LQ6k8wRcTj99SKGvxG5jD7QNb9ipjOWMkNHq2INzbyjlKTcqGP13Y
ATQts90TbXzTVAw1wOj1BXkuz28FozX6tQEEBnm1V4eCSeloFD6ZxLNaKtwstLeq
wT/m/w7ZQ+UFwIV1YmvVlNopcop+rpFKFWUeVCHe
-----END CERTIFICATE-----`
