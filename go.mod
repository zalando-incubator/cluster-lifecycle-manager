module github.com/zalando-incubator/cluster-lifecycle-manager

require (
	github.com/aws/aws-sdk-go v1.38.51
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/go-openapi/analysis v0.20.1 // indirect
	github.com/go-openapi/errors v0.20.0
	github.com/go-openapi/runtime v0.19.29
	github.com/go-openapi/strfmt v0.20.1
	github.com/go-openapi/swag v0.19.15
	github.com/go-openapi/validate v0.20.2
	github.com/go-swagger/go-swagger v0.27.0
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/googleapis/gnostic v0.5.4 // indirect
	github.com/mitchellh/copystructure v1.2.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.23.0 // indirect
	github.com/sirupsen/logrus v1.8.1
	github.com/spotinst/spotinst-sdk-go v1.88.0
	github.com/stretchr/testify v1.7.0
	github.com/zalando-incubator/kube-ingress-aws-controller v0.11.32
	golang.org/x/net v0.0.0-20210331212208-0fccb6fa2b5c // indirect
	golang.org/x/oauth2 v0.0.0-20210323180902-22b0adad7558
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20210503080704-8803ae5d1324 // indirect
	golang.org/x/term v0.0.0-20210317153231-de623e64d2a6 // indirect
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba // indirect
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/square/go-jose.v2 v2.5.1
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.19.10
	k8s.io/apimachinery v0.19.10
	k8s.io/client-go v0.19.10
	k8s.io/klog/v2 v2.8.0 // indirect
	k8s.io/utils v0.0.0-20210305010621-2afb4311ab10 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.1.0 // indirect
)

go 1.16
