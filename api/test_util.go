package api

func SampleCluster() *Cluster {
	return &Cluster{
		ID:                    "aws:123456789012:eu-central-1:kube-1",
		InfrastructureAccount: "aws:123456789012",
		LocalID:               "kube-1",
		APIServerURL:          "https://kube-1.foo.example.org/",
		Channel:               "alpha",
		Environment:           "production",
		CriticalityLevel:      1,
		LifecycleStatus:       "ready",
		Provider:              "zalando-aws",
		Region:                "eu-central-1",
		ConfigItems: map[string]string{
			"product_x_key": "abcde",
			"product_y_key": "12345",
		},
		NodePools: []*NodePool{
			{
				Name:             "master-default",
				Profile:          "master-default",
				InstanceTypes:    []string{"m4.large"},
				DiscountStrategy: "none",
				MinSize:          2,
				MaxSize:          2,
				ConfigItems:      map[string]string{},
			},
			{
				Name:             "worker-default",
				Profile:          "worker-default",
				InstanceTypes:    []string{"m5.large", "m5.2xlarge"},
				DiscountStrategy: "none",
				MinSize:          3,
				MaxSize:          21,
				ConfigItems: map[string]string{
					"taints": "my-taint=:NoSchedule",
				},
			},
		},
	}
}
