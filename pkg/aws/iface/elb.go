package iface

import "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"

type ELBAPI interface {
	elasticloadbalancing.DescribeLoadBalancersAPIClient
	elasticloadbalancing.DescribeInstanceHealthAPIClient
}
