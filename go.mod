module github.com/zalando-incubator/cluster-lifecycle-manager

require (
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751 // indirect
	github.com/alecthomas/units v0.0.0-20190717042225-c3de453c63f4 // indirect
	github.com/aws/aws-sdk-go v1.33.5
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/go-openapi/errors v0.19.6
	github.com/go-openapi/runtime v0.19.16
	github.com/go-openapi/strfmt v0.19.5
	github.com/go-openapi/swag v0.19.9
	github.com/go-openapi/validate v0.19.10
	github.com/go-swagger/go-swagger v0.24.0
	github.com/googleapis/gnostic v0.2.0 // indirect
	github.com/jteeuwen/go-bindata v3.0.8-0.20180305030458-6025e8de665b+incompatible
	github.com/mitchellh/copystructure v0.0.0-20170525013902-d23ffcb85de3
	github.com/mitchellh/reflectwalk v0.0.0-20170726202117-63d60e9d0dbc // indirect
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.6.0
	github.com/spotinst/spotinst-sdk-go v1.54.0
	github.com/stretchr/testify v1.6.1
	github.com/zalando-incubator/kube-ingress-aws-controller v0.11.1
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/square/go-jose.v2 v2.5.1
	gopkg.in/yaml.v2 v2.3.0
	k8s.io/api v0.17.0
	k8s.io/apimachinery v0.17.0
	k8s.io/client-go v0.17.0
)

go 1.13
