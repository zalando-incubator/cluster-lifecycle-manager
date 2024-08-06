package registry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
	"golang.org/x/oauth2"
)

const (
	testClusterID = "testcluster"
)

func setupRegistry(clusterInRegistry *models.Cluster) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc(
		"/kubernetes-clusters/"+*clusterInRegistry.ID,
		func(res http.ResponseWriter, req *http.Request) {
			var clusterUpdate models.ClusterUpdate
			if req.Method != http.MethodPatch {
				res.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			if req.Body == nil {
				res.WriteHeader(http.StatusBadRequest)
				return
			}
			defer req.Body.Close()

			if err := json.NewDecoder(req.Body).Decode(&clusterUpdate); err != nil {
				res.WriteHeader(http.StatusBadRequest)
				return
			}

			clusterInRegistry.ConfigItems = clusterUpdate.ConfigItems
			clusterInRegistry.LifecycleStatus = &clusterUpdate.LifecycleStatus
			clusterInRegistry.Status = clusterUpdate.Status
		},
	)

	ts := httptest.NewServer(mux)

	return ts
}

func TestUpdateConfigItems(t *testing.T) {
	for _, tc := range []struct {
		configItems map[string]string
	}{
		{
			configItems: map[string]string{
				"foo": "fighters",
			},
		},
		{
			configItems: map[string]string{
				"eks_endpoint": "https://api.eks.eu-central-1.amazonaws.com",
			},
		},
		{
			configItems: map[string]string{
				"eks_endpoint":                   "https://api.eks.eu-central-1.amazonaws.com",
				"eks_certificate_authority_data": "YmxhaA==",
			},
		},
	} {
		clusterInRegistry := &models.Cluster{
			ID: aws.String(testClusterID),
			ConfigItems: map[string]string{
				"foo": "bar",
			},
		}

		ts := setupRegistry(clusterInRegistry)
		defer ts.Close()

		registry := NewRegistry(
			ts.URL,
			oauth2.StaticTokenSource(&oauth2.Token{}),
			&Options{},
		)
		if registry == nil {
			t.Errorf("failed to create Registry")
			continue
		}

		err := registry.UpdateConfigItems(
			&api.Cluster{ID: testClusterID, ConfigItems: tc.configItems},
		)

		require.NoError(t, err)
		for key := range tc.configItems {
			require.Contains(t, clusterInRegistry.ConfigItems, key)
			require.Equal(t, tc.configItems[key], clusterInRegistry.ConfigItems[key])
		}
	}
}

func TestUpdateLifeCycleStatus(t *testing.T) {
	for _, tc := range []struct {
		lifecycleStatus string
		status          *api.ClusterStatus
	}{
		{
			lifecycleStatus: "ready",
			status: &api.ClusterStatus{
				CurrentVersion: "bbbb",
				LastVersion:    "xoxo",
				NextVersion:    "cccc",
			},
		},
		{
			lifecycleStatus: "decommissioned",
			status: &api.ClusterStatus{
				CurrentVersion: "aaaa",
				LastVersion:    "xoxo",
				NextVersion:    "bbbb",
			},
		},
	} {
		clusterInRegistry := &models.Cluster{
			ID:              aws.String(testClusterID),
			LifecycleStatus: aws.String("provisioning"),
			Status: &models.ClusterStatus{
				CurrentVersion: "aaaa",
				LastVersion:    "xoxo",
				NextVersion:    "bbbb",
			},
		}

		ts := setupRegistry(clusterInRegistry)
		defer ts.Close()

		registry := NewRegistry(
			ts.URL,
			oauth2.StaticTokenSource(&oauth2.Token{}),
			&Options{},
		)
		if registry == nil {
			t.Errorf("failed to create Registry")
			continue
		}

		err := registry.UpdateLifecycleStatus(
			&api.Cluster{
				ID:              testClusterID,
				LifecycleStatus: tc.lifecycleStatus,
				Status:          tc.status,
			},
		)

		require.NoError(t, err)
		require.Equal(t, tc.lifecycleStatus, *clusterInRegistry.LifecycleStatus)
		require.Equal(
			t,
			tc.status.CurrentVersion,
			clusterInRegistry.Status.CurrentVersion,
		)
		require.Equal(
			t,
			tc.status.LastVersion,
			clusterInRegistry.Status.LastVersion,
		)
		require.Equal(
			t,
			tc.status.NextVersion,
			clusterInRegistry.Status.NextVersion,
		)
	}
}
