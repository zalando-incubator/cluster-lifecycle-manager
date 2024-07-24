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
	nextVersion                         = "version"
	mockProvider provisioner.ProviderID = "<mock>"
)

var defaultLogger = log.WithFields(map[string]interface{}{})

type mockProvisioner struct{}

func (p *mockProvisioner) Supports(cluster *api.Cluster) bool {
	return cluster.Provider == string(mockProvider)
}

func (p *mockProvisioner) Provision(
	_ context.Context,
	_ *log.Entry,
	_ *api.Cluster,
	_ channel.Config,
) error {
	return nil
}

func (p *mockProvisioner) Decommission(_ context.Context, _ *log.Entry, _ *api.Cluster) error {
	return nil
}

type mockErrProvisioner mockProvisioner

func (p *mockErrProvisioner) Supports(_ *api.Cluster) bool {
	return true
}

func (p *mockErrProvisioner) Provision(
	_ context.Context,
	_ *log.Entry,
	_ *api.Cluster,
	_ channel.Config,
) error {
	return fmt.Errorf("failed to provision")
}

func (p *mockErrProvisioner) Decommission(_ context.Context, _ *log.Entry, _ *api.Cluster) error {
	return fmt.Errorf("failed to decommission")
}

type mockErrCreateProvisioner struct{ *mockProvisioner }

func (p *mockErrCreateProvisioner) Supports(_ *api.Cluster) bool {
	return true
}

func (p *mockErrCreateProvisioner) Provision(
	_ context.Context,
	_ *log.Entry,
	_ *api.Cluster,
	_ channel.Config,
) error {
	return fmt.Errorf("failed to provision")
}

type mockCountingErrProvisioner struct {
	*mockProvisioner
	attempt int
}

func (p *mockCountingErrProvisioner) Supports(_ *api.Cluster) bool {
	return true
}

func (p *mockCountingErrProvisioner) Provision(
	_ context.Context,
	_ *log.Entry,
	_ *api.Cluster,
	_ channel.Config,
) error {
	p.attempt++
	return fmt.Errorf("attempt %d failed to provision", p.attempt)
}

func (p *mockCountingErrProvisioner) Decommission(_ context.Context, _ *log.Entry, _ *api.Cluster) error {
	return fmt.Errorf("failed to decommission")
}

type mockRegistry struct {
	theCluster *api.Cluster
	lastUpdate *api.Cluster
}

func createMockRegistry(lifecycleStatus string, status *api.ClusterStatus) *mockRegistry {
	if status == nil {
		status = &api.ClusterStatus{}
	}
	cluster := &api.Cluster{
		ID:                    "aws:123456789012:eu-central-1:kube-1",
		InfrastructureAccount: "aws:123456789012",
		Channel:               "alpha",
		LifecycleStatus:       lifecycleStatus,
		Status:                status,
		Provider:              string(mockProvider),
	}
	return &mockRegistry{theCluster: cluster}
}

func (r *mockRegistry) ListClusters(_ registry.Filter) ([]*api.Cluster, error) {
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

func (r *mockChannelSource) Update(_ context.Context, _ *log.Entry) error {
	return nil
}

func (r *mockChannelSource) Delete(_ *log.Entry, _ channel.Config) error {
	return nil
}

type mockVersion struct {
	failGet bool
}

func (r *mockVersion) ID() string {
	return "<some-sha>"
}

func (r *mockVersion) Get(_ context.Context, _ *log.Entry) (channel.Config, error) {
	if r.failGet {
		return nil, fmt.Errorf("failed to checkout version %s", r.ID())
	}
	return &mockConfig{}, nil
}

type mockConfig struct {
	mockManifest channel.Manifest
}

func (c *mockConfig) StackManifest(_ string) (channel.Manifest, error) {
	return c.mockManifest, nil
}

func (c *mockConfig) EtcdManifest(_ string) (channel.Manifest, error) {
	return c.mockManifest, nil
}

func (c *mockConfig) NodePoolManifest(_ string, _ string) (channel.Manifest, error) {
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
			registry:      createMockRegistry(statusRequested, nil),
			provisioner:   &mockProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       true,
		},
		{
			testcase:      "lifecycle status ready",
			registry:      createMockRegistry(statusReady, nil),
			provisioner:   &mockProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       true,
		},
		{
			testcase:      "lifecycle status decommission-requested",
			registry:      createMockRegistry(statusDecommissionRequested, nil),
			provisioner:   &mockProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       true,
		},
		{
			testcase:      "lifecycle status requested, provisioner.Create fails",
			registry:      createMockRegistry(statusRequested, &api.ClusterStatus{CurrentVersion: nextVersion}),
			provisioner:   &mockErrCreateProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       false,
		},
		{
			testcase:      "lifecycle status ready, version up to date fails",
			registry:      createMockRegistry(statusReady, &api.ClusterStatus{CurrentVersion: nextVersion}),
			provisioner:   &mockProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       true,
		},
		{
			testcase:      "lifecycle status ready, provisioner.Version failing",
			registry:      createMockRegistry(statusReady, nil),
			provisioner:   &mockErrProvisioner{},
			channelSource: MockChannelSource(false, false),
			options:       defaultOptions,
			success:       false,
		},
		{
			testcase:      "lifecycle status ready, channelSource.Version() fails",
			registry:      createMockRegistry(statusReady, nil),
			provisioner:   &mockErrProvisioner{},
			channelSource: MockChannelSource(true, false),
			options:       defaultOptions,
			success:       false,
		}, {
			testcase:      "lifecycle status ready, channelVersion.Get() fails",
			registry:      createMockRegistry(statusReady, nil),
			provisioner:   &mockErrProvisioner{},
			channelSource: MockChannelSource(false, true),
			options:       defaultOptions,
			success:       false,
		},
	} {
		controller := New(
			defaultLogger,
			command.NewExecManager(1),
			ti.registry,
			map[provisioner.ProviderID]provisioner.Provisioner{
				mockProvider: ti.provisioner,
			},
			ti.channelSource,
			ti.options,
		)
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
	registry := createMockRegistry("ready", nil)
	registry.theCluster.Provider = "<unsupported>"
	controller := New(
		defaultLogger,
		command.NewExecManager(1),
		registry,
		map[provisioner.ProviderID]provisioner.Provisioner{
			"<supported>": &mockProvisioner{},
		},
		MockChannelSource(false, false),
		defaultOptions,
	)

	err := controller.refresh()
	require.NoError(t, err)

	next := controller.clusterList.SelectNext(func() {})
	require.Nil(t, next)
}

func TestCoalesceFailures(t *testing.T) {
	t.Run("limits various problems", func(t *testing.T) {
		registry := createMockRegistry("ready", nil)
		controller := New(
			defaultLogger,
			command.NewExecManager(1),
			registry,
			map[provisioner.ProviderID]provisioner.Provisioner{
				mockProvider: &mockCountingErrProvisioner{},
			},
			MockChannelSource(false, false),
			defaultOptions,
		)

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
	})

	t.Run("compacts repeating problems", func(t *testing.T) {
		registry := createMockRegistry("ready", nil)
		controller := New(
			defaultLogger,
			command.NewExecManager(1),
			registry,
			map[provisioner.ProviderID]provisioner.Provisioner{
				mockProvider: &mockErrProvisioner{},
			},
			MockChannelSource(false, false),
			defaultOptions,
		)

		for i := 0; i < 100; i++ {
			err := controller.refresh()
			require.NoError(t, err)

			ctx, cancelFunc := context.WithCancel(context.Background())

			next := controller.clusterList.SelectNext(cancelFunc)
			require.NotNil(t, next)
			controller.processCluster(ctx, 0, next)

			registry.theCluster.Status = registry.lastUpdate.Status
			require.Len(t, registry.theCluster.Status.Problems, 1)
		}
	})
}
