package api

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
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
	case reflect.Slice:
		switch fld.Interface().(type) {
		case []string:
			fld.Set(reflect.ValueOf([]string{"a", "b", "c"}))
		default:
			return fmt.Errorf("invalid slice type for %s", field)
		}
	default:
		return fmt.Errorf("unsupported type: %s", fld.Type())
	}

	return nil
}

type mockVersion struct{}

func (v mockVersion) ID() string {
	return "git-commit-hash"
}

func (v mockVersion) Get(_ context.Context, _ *logrus.Entry) (channel.Config, error) {
	return nil, errors.New("unsupported")
}

func TestVersion(t *testing.T) {
	commitHash := mockVersion{}

	version, err := SampleCluster().Version(commitHash)
	require.NoError(t, err)

	// cluster fields
	fields, err := fieldNames(SampleCluster())
	require.NoError(t, err)

	for _, field := range fields {
		if field == "Alias" || field == "NodePools" || field == "Owner" || field == "AccountName" || field == "AccountClusters" || field == "Status" {
			continue
		}

		cluster := SampleCluster()
		err := permute(cluster, field)
		require.NoError(t, err, "cluster field: %s", field)

		newVersion, err := cluster.Version(commitHash)

		require.NoError(t, err, "cluster field: %s", field)
		require.NotEqual(t, version, newVersion, "cluster field: %s", field)
	}

	// node pool fields
	fields, err = fieldNames(SampleCluster().NodePools[0])
	require.NoError(t, err)

	for _, field := range fields {
		if field == "InstanceType" {
			continue
		}

		cluster := SampleCluster()
		err := permute(cluster.NodePools[0], field)
		require.NoError(t, err, "node pool field: %s", field)

		newVersion, err := cluster.Version(commitHash)

		require.NoError(t, err, "node pool field: %s", field)
		require.NotEqual(t, version, newVersion, "cluster field: %s", field)
	}
}

func TestName(t *testing.T) {
	cluster := &Cluster{
		ID:       "aws:123456789012:eu-central-1:test-cluster",
		LocalID:  "test-cluster",
		Provider: ZalandoAWSProvider,
	}

	require.Equal(t, cluster.ID, cluster.Name())

	cluster = &Cluster{
		ID:       "aws:123456789012:eu-central-1:test-cluster",
		LocalID:  "test-cluster",
		Provider: ZalandoEKSProvider,
	}

	require.Equal(t, cluster.LocalID, cluster.Name())
}

func TestInfrastructureAccountID(t *testing.T) {
	cluster := &Cluster{InfrastructureAccount: "aws:123456789012"}
	assert.Equal(t, "123456789012", cluster.InfrastructureAccountID())
}
func TestWorkerRoleARN(t *testing.T) {
	cluster := &Cluster{InfrastructureAccount: "aws:123456789012", LocalID: "kube-1"}
	assert.Equal(t, "arn:aws:iam::123456789012:role/kube-1-worker", cluster.WorkerRoleARN())
}

func TestOIDCProvider(t *testing.T) {
	cluster := &Cluster{
		Provider:     ZalandoAWSProvider,
		LocalID:      "kube-1",
		APIServerURL: "https://kube-1.example.zalan.do",
	}
	assert.Equal(t, "kube-1.example.zalan.do", cluster.OIDCProvider())

	cluster = &Cluster{
		Provider: ZalandoEKSProvider,
		ConfigItems: map[string]string{
			"eks_oidc_issuer_url": "https://oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888",
		},
	}
	assert.Equal(t, "oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888", cluster.OIDCProvider())
}

func TestOIDCProviderARN(t *testing.T) {
	cluster := &Cluster{
		Provider:              ZalandoAWSProvider,
		LocalID:               "kube-1",
		InfrastructureAccount: "aws:123456789012",
		APIServerURL:          "https://kube-1.example.zalan.do",
	}
	assert.Equal(t, "arn:aws:iam::123456789012:oidc-provider/kube-1.example.zalan.do", cluster.OIDCProviderARN())

	cluster = &Cluster{
		Provider:              ZalandoEKSProvider,
		InfrastructureAccount: "aws:123456789012",
		ConfigItems: map[string]string{
			"eks_oidc_issuer_url": "https://oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888",
		},
	}
	assert.Equal(t, "arn:aws:iam::123456789012:oidc-provider/oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888", cluster.OIDCProviderARN())
}

func TestOIDCSubjectKey(t *testing.T) {
	cluster := &Cluster{
		Provider:     ZalandoAWSProvider,
		LocalID:      "kube-1",
		APIServerURL: "https://kube-1.example.zalan.do",
	}
	assert.Equal(t, "kube-1.example.zalan.do:sub", cluster.OIDCSubjectKey())

	cluster = &Cluster{
		Provider: ZalandoEKSProvider,
		ConfigItems: map[string]string{
			"eks_oidc_issuer_url": "https://oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888",
		},
	}
	assert.Equal(t, "oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888:sub", cluster.OIDCSubjectKey())
}

func TestTrustRelationship(t *testing.T) {
	legacyCluster := &Cluster{
		Provider:              ZalandoAWSProvider,
		LocalID:               "kube-1",
		InfrastructureAccount: "aws:123456789012",
		APIServerURL:          "https://kube-1.example.zalan.do",
	}
	legacyCluster.AccountClusters = []*Cluster{legacyCluster}

	legacyTrustRelationship := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"},{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:role/kube-1-worker"},"Action":"sts:AssumeRole"},{"Effect":"Allow","Principal":{"Federated":"arn:aws:iam::123456789012:oidc-provider/kube-1.example.zalan.do"},"Action":"sts:AssumeRoleWithWebIdentity","Condition":{"StringLike":{"kube-1.example.zalan.do:sub":"system:serviceaccount:${SERVICE_ACCOUNT}"}}}]}`
	assert.Equal(t, legacyTrustRelationship, legacyCluster.IAMRoleTrustRelationshipTemplate())

	eksCluster := &Cluster{
		Provider:              ZalandoEKSProvider,
		LocalID:               "teapot-euc1",
		InfrastructureAccount: "aws:123456789012",
		ConfigItems: map[string]string{
			"eks_oidc_issuer_url": "https://oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888",
		},
	}
	eksCluster.AccountClusters = []*Cluster{eksCluster}

	eksTrustRelationship := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"},{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:role/teapot-euc1-worker"},"Action":"sts:AssumeRole"},{"Effect":"Allow","Principal":{"Federated":"arn:aws:iam::123456789012:oidc-provider/oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888"},"Action":"sts:AssumeRoleWithWebIdentity","Condition":{"StringLike":{"oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888:sub":"system:serviceaccount:${SERVICE_ACCOUNT}"}}}]}`
	assert.Equal(t, eksTrustRelationship, eksCluster.IAMRoleTrustRelationshipTemplate())

	combinedAccountClusters := []*Cluster{legacyCluster, eksCluster}
	combinedTrustRelationship := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"},{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:role/kube-1-worker"},"Action":"sts:AssumeRole"},{"Effect":"Allow","Principal":{"Federated":"arn:aws:iam::123456789012:oidc-provider/kube-1.example.zalan.do"},"Action":"sts:AssumeRoleWithWebIdentity","Condition":{"StringLike":{"kube-1.example.zalan.do:sub":"system:serviceaccount:${SERVICE_ACCOUNT}"}}},{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:role/teapot-euc1-worker"},"Action":"sts:AssumeRole"},{"Effect":"Allow","Principal":{"Federated":"arn:aws:iam::123456789012:oidc-provider/oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888"},"Action":"sts:AssumeRoleWithWebIdentity","Condition":{"StringLike":{"oidc.eks.eu-central-1.amazonaws.com/id/11112222333344445555666677778888:sub":"system:serviceaccount:${SERVICE_ACCOUNT}"}}}]}`

	legacyCluster.AccountClusters = combinedAccountClusters
	assert.Equal(t, combinedTrustRelationship, legacyCluster.IAMRoleTrustRelationshipTemplate())

	eksCluster.AccountClusters = combinedAccountClusters
	assert.Equal(t, combinedTrustRelationship, eksCluster.IAMRoleTrustRelationshipTemplate())
}
