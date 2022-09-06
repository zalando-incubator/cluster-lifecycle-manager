package controller

import (
	"context"
	"fmt"
	"math"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"github.com/zalando-incubator/cluster-lifecycle-manager/provisioner"
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
)

const (
	nextVersion  = "version"
	mockProvider = "<mock>"
)

var defaultLogger = log.WithFields(map[string]interface{}{})

type mockProvisioner struct{}

func (p *mockProvisioner) Supports(cluster *api.Cluster) bool {
	return cluster.Provider == mockProvider
}

func (p *mockProvisioner) Provision(ctx context.Context, logger *log.Entry, cluster *api.Cluster, config channel.Config) error {
	return nil
}

func (p *mockProvisioner) Decommission(ctx context.Context, logger *log.Entry, cluster *api.Cluster) error {
	return nil
}

type mockErrProvisioner mockProvisioner

func (p *mockErrProvisioner) Supports(cluster *api.Cluster) bool {
	return true
}

func (p *mockErrProvisioner) Provision(ctx context.Context, logger *log.Entry, cluster *api.Cluster, config channel.Config) error {
	return fmt.Errorf("failed to provision")
}

func (p *mockErrProvisioner) Decommission(ctx context.Context, logger *log.Entry, cluster *api.Cluster) error {
	return fmt.Errorf("failed to decommission")
}

type mockErrCreateProvisioner struct{ *mockProvisioner }

func (p *mockErrCreateProvisioner) Supports(cluster *api.Cluster) bool {
	return true
}

func (p *mockErrCreateProvisioner) Provision(ctx context.Context, logger *log.Entry, cluster *api.Cluster, config channel.Config) error {
	return fmt.Errorf("failed to provision")
}

type mockRegistry struct {
	theCluster *api.Cluster
	lastUpdate *api.Cluster
}

func MockRegistry(lifecycleStatus string, status *api.ClusterStatus) *mockRegistry {
	if status == nil {
		status = &api.ClusterStatus{}
	}
	cluster := &api.Cluster{
		ID:                    "aws:123456789012:eu-central-1:kube-1",
		InfrastructureAccount: "aws:123456789012",
		Channel:               "alpha",
		LifecycleStatus:       lifecycleStatus,
		Status:                status,
		Provider:              mockProvider,
	}
	return &mockRegistry{theCluster: cluster}
}

func (r *mockRegistry) ListClusters(filter registry.Filter) ([]*api.Cluster, error) {
	return []*api.Cluster{r.theCluster}, nil
}
func (r *mockRegistry) UpdateCluster(cluster *api.Cluster) error {
	r.lastUpdate = cluster
	return nil
}

type mockChannelSource struct {
	failVersion bool
	failGet     bool
}

func MockChannelSource(failVersion, failGet bool) channel.ConfigSource {
	return &mockChannelSource{
		failVersion: failVersion,
		failGet:     failGet,
	}
}

func (r *mockChannelSource) Name() string {
	return "mock"
}

func (r *mockChannelSource) Version(channel string, overrides map[string]string) (channel.ConfigVersion, error) {
	if r.failVersion {
		return nil, fmt.Errorf("failed to get version %s (%s)", channel, overrides)
	}
	return &mockVersion{failGet: r.failGet}, nil
}

func (r *mockChannelSource) Update(ctx context.Context, logger *log.Entry) error {
	return nil
}

func (r *mockChannelSource) Delete(logger *log.Entry, config channel.Config) error {
	return nil
}

type mockVersion struct {
	failGet bool
}

func (r *mockVersion) ID() string {
	return "<some-sha>"
}

func (r *mockVersion) Get(ctx context.Context, logger *log.Entry) (channel.Config, error) {
	if r.failGet {
		return nil, fmt.Errorf("failed to checkout version %s", r.ID())
	}
	return &mockConfig{}, nil
}

type mockConfig struct {
	mockManifest channel.Manifest
}

func (c *mockConfig) StackManifest(manifestName string) (channel.Manifest, error) {
	return c.mockManifest, nil
}

func (c *mockConfig) EtcdManifest(manifestName string) (channel.Manifest, error) {
	return c.mockManifest, nil
}

func (c *mockConfig) NodePoolManifest(profileName string, manifestName string) (channel.Manifest, error) {
	return c.mockManifest, nil
}

func (c *mockConfig) DefaultsManifests() ([]channel.Manifest, error) {
	return []channel.Manifest{c.mockManifest}, nil
}

func (c *mockConfig) DeletionsManifests() ([]channel.Manifest, error) {
	return []channel.Manifest{c.mockManifest}, nil
}

func (c *mockConfig) Components() ([]channel.Component, error) {
	return nil, nil
}

func (c *mockConfig) Delete() error {
	return nil
}

var defaultOptions = &Options{
	AccountFilter: config.DefaultFilter,
}

func TestProcessCluster(t *testing.T) {
	for _, ti := range []struct {
		testcase      string
		registry      registry.Registry
		provisioner   provisioner.Provisioner
		channelSource channel.ConfigSource
		options       *Options
		success       bool
	}{
		{
			testcase:      "lifecycle status requested",
			registry:      MockRegistry(statusRequested, nil),
			provisioner:   &mockProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       true,
		},
		{
			testcase:      "lifecycle status ready",
			registry:      MockRegistry(statusReady, nil),
			provisioner:   &mockProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       true,
		},
		{
			testcase:      "lifecycle status decommission-requested",
			registry:      MockRegistry(statusDecommissionRequested, nil),
			provisioner:   &mockProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       true,
		},
		{
			testcase:      "lifecycle status requested, provisioner.Create fails",
			registry:      MockRegistry(statusRequested, &api.ClusterStatus{CurrentVersion: nextVersion}),
			provisioner:   &mockErrCreateProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       false,
		},
		{
			testcase:      "lifecycle status ready, version up to date fails",
			registry:      MockRegistry(statusReady, &api.ClusterStatus{CurrentVersion: nextVersion}),
			provisioner:   &mockProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       true,
		},
		{
			testcase:      "lifecycle status ready, provisioner.Version failing",
			registry:      MockRegistry(statusReady, nil),
			provisioner:   &mockErrProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       false,
		},
		{
			testcase:      "lifecycle status ready, channelSource.Version() fails",
			registry:      MockRegistry(statusReady, nil),
			provisioner:   &mockErrProvisioner{},
			channelSource: MockChannelSource(true, false),
			options:       defaultOptions,
			success:       false,
		}, {
			testcase:      "lifecycle status ready, channelVersion.Get() fails",
			registry:      MockRegistry(statusReady, nil),
			provisioner:   &mockErrProvisioner{},
			channelSource: MockChannelSource(false, true),
			options:       defaultOptions,
			success:       false,
		},
	} {
		controller := New(defaultLogger, command.NewExecManager(1), ti.registry, ti.provisioner, ti.channelSource, ti.options)
		err := controller.refresh()
		assert.NoError(t, err)

		ctx, cancelFunc := context.WithCancel(context.Background())

		next := controller.clusterList.SelectNext(cancelFunc)
		if !assert.NotNil(t, next, ti.testcase) {
			continue
		}

		err = controller.doProcessCluster(ctx, defaultLogger, next)
		if ti.success {
			assert.NoError(t, err, ti.testcase)
		} else {
			assert.Error(t, err, ti.testcase)
		}
	}
}

func TestIgnoreUnsupportedProvider(t *testing.T) {
	registry := MockRegistry("ready", nil)
	registry.theCluster.Provider = "<unsupported>"
	controller := New(defaultLogger, command.NewExecManager(1), registry, &mockProvisioner{}, MockChannelSource(false, false), defaultOptions)

	err := controller.refresh()
	require.NoError(t, err)

	next := controller.clusterList.SelectNext(func() {})
	require.Nil(t, next)
}

func TestCoalesceFailures(t *testing.T) {
	registry := MockRegistry("ready", nil)
	controller := New(defaultLogger, command.NewExecManager(1), registry, &mockErrProvisioner{}, MockChannelSource(false, false), defaultOptions)

	for i := 0; i < 100; i++ {
		err := controller.refresh()
		require.NoError(t, err)

		ctx, cancelFunc := context.WithCancel(context.Background())

		next := controller.clusterList.SelectNext(cancelFunc)
		require.NotNil(t, next)
		controller.processCluster(ctx, 0, next)

		registry.theCluster.Status = registry.lastUpdate.Status
		require.EqualValues(t, math.Min(errorLimit, float64(i+1)), len(registry.theCluster.Status.Problems))
	}
}
