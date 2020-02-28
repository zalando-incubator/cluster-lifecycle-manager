package channel

import (
	"context"
	"strings"

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

func (c *CachingSource) Version(channel string, overrides map[string]string) (ConfigVersion, error) {
	cacheKey := strings.Builder{}
	cacheKey.WriteString(channel)
	cacheKey.WriteRune('\x00')
	for k, v := range overrides {
		cacheKey.WriteString(k)
		cacheKey.WriteRune('\x00')
		cacheKey.WriteString(v)
		cacheKey.WriteRune('\x00')
	}

	if cached, ok := c.cache[cacheKey.String()]; ok {
		return cached, nil
	}
	res, err := c.target.Version(channel, overrides)
	if err != nil {
		return nil, err
	}
	c.cache[cacheKey.String()] = res
	return res, nil
}
