package channel

import (
	"context"

	"github.com/sirupsen/logrus"
)

// CachingSource caches resolved versions until the next Update()
type CachingSource struct {
	cache  map[string]ConfigVersion
	target ConfigSource
}

func NewCachingSource(target ConfigSource) *CachingSource {
	return &CachingSource{
		cache:  make(map[string]ConfigVersion),
		target: target,
	}
}

func (c *CachingSource) Name() string {
	return c.target.Name()
}

func (c *CachingSource) Update(ctx context.Context, logger *logrus.Entry) error {
	c.cache = make(map[string]ConfigVersion)
	return c.target.Update(ctx, logger)
}

func (c *CachingSource) Version(channel string) (ConfigVersion, error) {
	if cached, ok := c.cache[channel]; ok {
		return cached, nil
	}
	res, err := c.target.Version(channel)
	if err != nil {
		return nil, err
	}
	c.cache[channel] = res
	return res, nil
}
