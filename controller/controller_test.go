package controller

import (
	"fmt"
	"testing"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	"github.com/zalando-incubator/cluster-lifecycle-manager/provisioner"
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
)

const nextVersion = "version"

type mockProvisioner struct{}

func (p *mockProvisioner) Version(cluster *api.Cluster, config *channel.Config) (string, error) {
	return nextVersion, nil
}

func (p *mockProvisioner) Provision(cluster *api.Cluster, config *channel.Config) error {
	return nil
}

func (p *mockProvisioner) Decommission(cluster *api.Cluster, config *channel.Config) error {
	return nil
}

type mockErrProvisioner mockProvisioner

func (p *mockErrProvisioner) Version(cluster *api.Cluster, config *channel.Config) (string, error) {
	return "", fmt.Errorf("failed getting version")
}

func (p *mockErrProvisioner) Provision(cluster *api.Cluster, config *channel.Config) error {
	return fmt.Errorf("failed to provision")
}

func (p *mockErrProvisioner) Decommission(cluster *api.Cluster, config *channel.Config) error {
	return fmt.Errorf("failed to decommission")
}

type mockErrCreateProvisioner struct{ *mockProvisioner }

func (p *mockErrCreateProvisioner) Provision(cluster *api.Cluster, config *channel.Config) error {
	return fmt.Errorf("failed to provision")
}

type mockRegistry struct{}

func (r *mockRegistry) ListClusters(filter registry.Filter) ([]*api.Cluster, error) {
	return nil, nil
}
func (r *mockRegistry) UpdateCluster(cluster *api.Cluster) error { return nil }

type mockChannelSource struct{}

func (r *mockChannelSource) Get(ch string) (*channel.Config, error) {
	return &channel.Config{Version: nextVersion}, nil
}

func (r *mockChannelSource) Update() error {
	return nil
}

func (r *mockChannelSource) Delete(config *channel.Config) error {
	return nil
}

type mockErrChannelSource struct{}

func (r *mockErrChannelSource) Update() error {
	return fmt.Errorf("failed to update channel config")
}

func (r *mockErrChannelSource) Get(channel string) (*channel.Config, error) {
	return nil, fmt.Errorf("failed to get channel config")
}

func (r *mockErrChannelSource) Delete(config *channel.Config) error {
	return fmt.Errorf("failed to delete config")
}

var defaultOptions = &Options{
	AccountFilter: config.DefaultFilter,
}

func TestProcessCluster(t *testing.T) {
	cluster := &api.Cluster{
		ID: "aws:123456789012:eu-central-1:kube-1",
		InfrastructureAccount: "aws:123456789012",
		Channel:               "alpha",
		LifecycleStatus:       "ready",
	}

	for _, ti := range []struct {
		registry        registry.Registry
		provisioner     provisioner.Provisioner
		channelSource   channel.ConfigSource
		clusterStatus   *api.ClusterStatus
		options         *Options
		lifecycleStatus string
		success         bool
	}{
		// test when lifecyclestatus is requested
		{
			registry:        &mockRegistry{},
			provisioner:     &mockProvisioner{},
			channelSource:   &mockChannelSource{},
			lifecycleStatus: statusRequested,
			options:         defaultOptions,
			success:         true,
		},
		// test when lifecyclestatus is ready
		{
			registry:        &mockRegistry{},
			provisioner:     &mockProvisioner{},
			channelSource:   &mockChannelSource{},
			lifecycleStatus: statusReady,
			options:         defaultOptions,
			success:         true,
		},
		// test when lifecyclestatus is decommission-requested
		{
			registry:        &mockRegistry{},
			provisioner:     &mockProvisioner{},
			channelSource:   &mockChannelSource{},
			lifecycleStatus: statusDecommissionRequested,
			options:         defaultOptions,
			success:         true,
		},
		// test when lifecyclestatus is decommissioned
		{
			registry:        &mockRegistry{},
			provisioner:     &mockProvisioner{},
			channelSource:   &mockChannelSource{},
			lifecycleStatus: statusDecommissioned,
			options:         defaultOptions,
			success:         true,
		},
		// test when lifecyclestatus is requested and provisoner.Create
		// fails
		{
			registry:        &mockRegistry{},
			provisioner:     &mockErrCreateProvisioner{},
			channelSource:   &mockChannelSource{},
			clusterStatus:   &api.ClusterStatus{CurrentVersion: nextVersion},
			lifecycleStatus: statusRequested,
			options:         defaultOptions,
			success:         false,
		},
		// test when lifecyclestatus is requested and provisoner.Create
		// fails
		{
			registry:        &mockRegistry{},
			provisioner:     &mockErrCreateProvisioner{},
			channelSource:   &mockChannelSource{},
			clusterStatus:   &api.ClusterStatus{CurrentVersion: nextVersion},
			lifecycleStatus: statusRequested,
			options:         defaultOptions,
			success:         false,
		},
		// test when lifecyclestatus is ready and version is up to date
		// fails
		{
			registry:        &mockRegistry{},
			provisioner:     &mockProvisioner{},
			channelSource:   &mockChannelSource{},
			clusterStatus:   &api.ClusterStatus{CurrentVersion: nextVersion},
			lifecycleStatus: statusReady,
			options:         defaultOptions,
			success:         true,
		},
		// test when lifecyclestatus is ready and provisioner.Version
		// is failing.
		{
			registry:        &mockRegistry{},
			provisioner:     &mockErrProvisioner{},
			channelSource:   &mockChannelSource{},
			lifecycleStatus: statusReady,
			options:         defaultOptions,
			success:         false,
		},
		// test when channel.Channel fails.
		{
			registry:        &mockRegistry{},
			provisioner:     &mockErrProvisioner{},
			channelSource:   &mockErrChannelSource{},
			lifecycleStatus: statusReady,
			options:         defaultOptions,
			success:         false,
		},
	} {
		controller := New(ti.registry, ti.provisioner, ti.channelSource, ti.options)
		cluster.LifecycleStatus = ti.lifecycleStatus
		cluster.Status = ti.clusterStatus
		err := controller.doProcessCluster(cluster)
		if err != nil && ti.success {
			t.Errorf("should not fail: %s", err)
		}

		if err == nil && !ti.success {
			t.Errorf("expected failure")
		}
	}
}
