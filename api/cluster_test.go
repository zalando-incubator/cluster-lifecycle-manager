package api

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
)

func fieldNames(value interface{}) ([]string, error) {
	v := reflect.ValueOf(value)

	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("invalid value type, expected pointer to struct: %s", v.Kind())
	}

	v = v.Elem()

	result := make([]string, v.Type().NumField())
	for i := 0; i < v.NumField(); i++ {
		result[i] = v.Type().Field(i).Name
	}
	return result, nil
}

func permute(value interface{}, field string) error {
	v := reflect.ValueOf(value)

	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("invalid value type, expected pointer to struct: %s", v.Kind())
	}

	fld := v.Elem().FieldByName(field)
	switch fld.Type().Kind() {
	case reflect.String:
		fld.SetString("<permuted>")
	case reflect.Int, reflect.Int32, reflect.Int64:
		fld.SetInt(123456)
	case reflect.Map:
		switch v := fld.Interface().(type) {
		case map[string]string:
			v["<permuted_key>"] = "permuted_value"
		default:
			return fmt.Errorf("invalid map type for %s", field)
		}
	default:
		return fmt.Errorf("unsupported type: %s", fld.Type())
	}

	return nil
}

func sampleCluster() *Cluster {
	return &Cluster{
		ID: "aws:123456789012:eu-central-1:kube-1",
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
		Outputs: map[string]string{},
		NodePools: []*NodePool{
			{
				Name:             "master-default",
				Profile:          "master/default",
				InstanceType:     "m3.medium",
				DiscountStrategy: "none",
				MinSize:          2,
				MaxSize:          2,
			},
			{
				Name:             "worker-default",
				Profile:          "worker/default",
				InstanceType:     "r4.large",
				DiscountStrategy: "none",
				MinSize:          3,
				MaxSize:          20,
			},
		},
	}
}

func TestVersion(t *testing.T) {
	commitHash := channel.ConfigVersion("git-commit-hash")

	version, err := sampleCluster().Version(commitHash)
	require.NoError(t, err)

	// cluster fields
	fields, err := fieldNames(sampleCluster())
	require.NoError(t, err)

	for _, field := range fields {
		if field == "Alias" || field == "NodePools" || field == "Outputs" || field == "Owner" || field == "Status" {
			continue
		}

		cluster := sampleCluster()
		err := permute(cluster, field)
		require.NoError(t, err, "cluster field: %s", field)

		newVersion, err := cluster.Version(commitHash)

		require.NoError(t, err, "cluster field: %s", field)
		require.NotEqual(t, version, newVersion, "cluster field: %s", field)
	}

	// node pool fields
	fields, err = fieldNames(sampleCluster().NodePools[0])
	require.NoError(t, err)

	for _, field := range fields {
		cluster := sampleCluster()
		err := permute(cluster.NodePools[0], field)
		require.NoError(t, err, "node pool field: %s", field)

		newVersion, err := cluster.Version(commitHash)

		require.NoError(t, err, "node pool field: %s", field)
		require.NotEqual(t, version, newVersion, "cluster field: %s", field)
	}
}
