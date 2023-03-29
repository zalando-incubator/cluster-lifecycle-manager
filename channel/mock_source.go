package channel

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
)

type mockSource struct {
	name          string
	validChannels []string
}

type mockVersion struct {
	channel string
}

func (s *mockSource) Name() string {
	return s.name
}

func (s *mockSource) Update(_ context.Context, _ *logrus.Entry) error {
	return nil
}

func (s *mockSource) Version(channel string, _ map[string]string) (ConfigVersion, error) {
	for _, validChannel := range s.validChannels {
		if validChannel == channel {
			return &mockVersion{channel: channel}, nil
		}
	}
	return nil, fmt.Errorf("unknown version: %s", channel)
}

func (v *mockVersion) ID() string {
	return v.channel
}

func (v *mockVersion) Get(_ context.Context, _ *logrus.Entry) (Config, error) {
	return nil, fmt.Errorf("not implemented")
}
