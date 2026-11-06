package channel

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func newTestChannelGit(t *testing.T, workdir, remote string) *ChannelGit {
	t.Helper()
	source, err := NewChannelGit("testsrc", workdir, remote, "", 15*time.Second, 30*24*time.Hour)
	require.NoError(t, err)
	return source.(*ChannelGit)
}

func forceRefresh(g *ChannelGit, channel string) {
	e := g.channels[cacheKey(channel)]
	e.mu.Lock()
	e.lastAttempt = time.Time{}
	e.mu.Unlock()
}

func TestChannelGitSnapshotsAndRefresh(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote")
	createGitRepo(t, log.NewEntry(log.StandardLogger()), remote)
	g := newTestChannelGit(t, t.TempDir(), remote)
	logger := log.NewEntry(log.StandardLogger())
	old, err := g.Version(context.Background(), "main", nil)
	require.NoError(t, err)
	oldConfig, err := old.Get(context.Background(), logger)
	require.NoError(t, err)
	verifyExampleConfig(t, oldConfig, "testsrc", "channel1")
	manifest, err := oldConfig.StackManifest("stack.yaml")
	require.NoError(t, err)
	manifest.Contents[0] = 'X'
	verifyExampleConfig(t, oldConfig, "testsrc", "channel1")
	addCommit(t, remote, "main", "channel1-v2", "second commit")
	unchanged, err := g.Version(context.Background(), "main", nil)
	require.NoError(t, err)
	require.Equal(t, old.ID(), unchanged.ID(), "refresh is throttled")
	forceRefresh(g, "main")
	current, err := g.Version(context.Background(), "main", nil)
	require.NoError(t, err)
	require.NotEqual(t, old.ID(), current.ID())
	currentConfig, err := current.Get(context.Background(), logger)
	require.NoError(t, err)
	verifyExampleConfig(t, currentConfig, "testsrc", "channel1-v2")
	verifyExampleConfig(t, oldConfig, "testsrc", "channel1")
	// A version retained by the scheduler must remain usable after refresh.
	oldAgain, err := old.Get(context.Background(), logger)
	require.NoError(t, err)
	verifyExampleConfig(t, oldAgain, "testsrc", "channel1")
	other, err := g.Version(context.Background(), "main", map[string]string{"testsrc": "channel2"})
	require.NoError(t, err)
	otherConfig, err := other.Get(context.Background(), logger)
	require.NoError(t, err)
	verifyExampleConfig(t, otherConfig, "testsrc", "channel2")
	require.NoError(t, oldConfig.Delete())
	verifyExampleConfig(t, oldAgain, "testsrc", "channel1")
}

func TestChannelGitOfflineRestartAndNoCache(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote")
	workdir := t.TempDir()
	createGitRepo(t, log.NewEntry(log.StandardLogger()), remote)
	g := newTestChannelGit(t, workdir, remote)
	v, err := g.Version(context.Background(), "main", nil)
	require.NoError(t, err)
	require.NoError(t, os.Rename(remote, remote+"-offline"))
	restarted := newTestChannelGit(t, workdir, remote)
	cached, err := restarted.Version(context.Background(), "main", nil)
	require.NoError(t, err)
	require.Equal(t, v.ID(), cached.ID())
	cfg, err := cached.Get(context.Background(), log.NewEntry(log.StandardLogger()))
	require.NoError(t, err)
	verifyExampleConfig(t, cfg, "testsrc", "channel1")
	_, err = restarted.Version(context.Background(), "channel2", nil)
	require.Error(t, err)
	// A failed initial clone must not prevent a later retry.
	require.NoError(t, os.Rename(remote+"-offline", remote))
	_, err = restarted.Version(context.Background(), "channel2", nil)
	require.Error(t, err, "failed attempts are also throttled")
	forceRefresh(restarted, "channel2")
	_, err = restarted.Version(context.Background(), "channel2", nil)
	require.NoError(t, err)
}

func TestChannelGitConcurrentAndGarbageCollection(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote")
	createGitRepo(t, log.NewEntry(log.StandardLogger()), remote)
	g := newTestChannelGit(t, t.TempDir(), remote)
	const count = 200
	versions := make([]ConfigVersion, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := range versions {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			versions[i], errs[i] = g.Version(context.Background(), "main", nil)
		}(i)
	}
	wg.Wait()
	for i := range versions {
		require.NoError(t, errs[i])
		require.Same(t, versions[0], versions[i])
	}
	_, err := g.Version(context.Background(), "channel2", nil)
	require.NoError(t, err)
	marker := filepath.Join(g.root, cacheKey("channel2"), "current")
	old := time.Now().Add(-31 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(marker, old, old))
	require.NoError(t, g.GarbageCollect(context.Background(), log.NewEntry(log.StandardLogger())))
	_, err = os.Stat(filepath.Dir(marker))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(g.root, cacheKey("main"), "current"))
	require.NoError(t, err)
}

func TestCachingSourceForwardsGarbageCollection(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote")
	createGitRepo(t, log.NewEntry(log.StandardLogger()), remote)
	g := newTestChannelGit(t, t.TempDir(), remote)
	cached := NewCachingSource(g)
	_, err := cached.Version(context.Background(), "main", nil)
	require.NoError(t, err)
	marker := filepath.Join(g.root, cacheKey("main"), "current")
	old := time.Now().Add(-31 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(marker, old, old))
	require.NoError(t, cached.GarbageCollect(context.Background(), log.NewEntry(log.StandardLogger())))
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err))
}

func TestChannelGitRejectsInvalidChannelAndIsolatesSources(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote")
	createGitRepo(t, log.NewEntry(log.StandardLogger()), remote)
	workdir := t.TempDir()
	g := newTestChannelGit(t, workdir, remote)
	_, err := g.Version(context.Background(), "../../outside", nil)
	require.Error(t, err)
	_, err = g.Version(context.Background(), "main", nil)
	require.NoError(t, err)
	other, err := NewChannelGit("other", workdir, remote, "", time.Second, time.Hour)
	require.NoError(t, err)
	require.NotEqual(t, g.root, other.(*ChannelGit).root)
}

func TestChannelGitSnapshotSymlinks(t *testing.T) {
	g := newTestChannelGit(t, t.TempDir(), "https://example.com/repo.git")
	root := t.TempDir()
	SetupConfig(t, root, map[string]string{"cluster/stack.yaml": "content"})
	require.NoError(t, os.Symlink("stack.yaml", filepath.Join(root, "cluster", "link.yaml")))
	v, err := g.snapshot(root, "abc")
	require.NoError(t, err)
	manifest, err := v.config.StackManifest("link.yaml")
	require.NoError(t, err)
	require.Equal(t, "content", string(manifest.Contents))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "outside")))
	_, err = g.snapshot(root, "abc")
	require.ErrorContains(t, err, "escapes checkout")
}

func TestChannelGitVersionCancelled(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote")
	createGitRepo(t, log.NewEntry(log.StandardLogger()), remote)
	g := newTestChannelGit(t, t.TempDir(), remote)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.Version(ctx, "main", nil)
	require.ErrorIs(t, err, context.Canceled)
}
