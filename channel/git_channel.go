package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing/fstest"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	log "github.com/sirupsen/logrus"
)

// ChannelGit keeps one persistent single-branch repository and checkout per
// channel. Versions hold immutable in-memory snapshots, not checkout paths.
// A cache directory must be used by only one CLM process at a time.
type ChannelGit struct {
	name, root, url  string
	git              *Git // authentication only
	interval, maxAge time.Duration
	mu               sync.Mutex
	channels         map[string]*channelGitEntry
}

type channelGitEntry struct {
	mu          sync.RWMutex
	users       int // protected by ChannelGit.mu
	lastAttempt time.Time
	lastErr     error
	version     *channelGitVersion
}

type channelGitVersion struct {
	sha    string
	config *SimpleConfig
}

func (v *channelGitVersion) ID() string { return v.sha }
func (v *channelGitVersion) Get(ctx context.Context, _ *log.Entry) (Config, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return v.config, nil
}

// NewChannelGit creates a source isolated by source name and repository URL.
func NewChannelGit(name, workdir, url, key string, interval, maxAge time.Duration) (ConfigSource, error) {
	if interval <= 0 || maxAge <= 0 {
		return nil, fmt.Errorf("git channel refresh interval and cache max age must be positive")
	}
	root, err := filepath.Abs(workdir)
	if err != nil {
		return nil, err
	}
	sshConfig := &Git{repositoryURL: url, sshPrivateKeyFile: key}
	if isSSHURL(url) {
		sshConfig.sshConfig, err = loadSSHConfig()
		if err != nil {
			log.Warnf("failed to load SSH config for %q, falling back to --ssh-private-key-path: %v", name, err)
		}
	}
	return &ChannelGit{name: name, url: url, root: filepath.Join(root, "git-channels", cacheKey(name+"\x00"+url)), git: sshConfig, interval: interval, maxAge: maxAge, channels: make(map[string]*channelGitEntry)}, nil
}

func cacheKey(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (g *ChannelGit) Name() string { return g.name }

// Update is intentionally lazy: each channel is refreshed when its version is requested.
func (g *ChannelGit) Update(ctx context.Context, _ *log.Entry) error { return ctx.Err() }

func (g *ChannelGit) Version(ctx context.Context, channel string, overrides map[string]string) (ConfigVersion, error) {
	if override, ok := overrides[g.name]; ok {
		channel = override
	}
	ref := plumbing.NewBranchReferenceName(channel)
	if err := ref.Validate(); err != nil || channel == "" {
		return nil, fmt.Errorf("invalid git channel %q", channel)
	}
	key := cacheKey(channel)
	g.mu.Lock()
	e := g.channels[key]
	if e == nil {
		e = &channelGitEntry{}
		g.channels[key] = e
	}
	e.users++
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		e.users--
		g.mu.Unlock()
	}()

	dir := filepath.Join(g.root, key)
	e.mu.RLock()
	if e.version != nil && time.Since(e.lastAttempt) < g.interval {
		version := e.version
		err := touchChannelCache(dir)
		e.mu.RUnlock()
		if err != nil {
			return nil, err
		}
		return version, nil
	}
	e.mu.RUnlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.version == nil {
		v, err := g.loadCached(dir)
		if err == nil {
			e.version = v
		}
	}
	if time.Since(e.lastAttempt) >= g.interval {
		v, err := g.refresh(ctx, dir, ref, e.version)
		e.lastAttempt = time.Now()
		e.lastErr = err
		if err == nil {
			e.version = v
		} else if e.version != nil {
			log.Warnf("failed to refresh git source %q channel %q; using cached version %s: %v", g.name, channel, e.version.sha, err)
		}
	}
	if e.version == nil {
		return nil, fmt.Errorf("no cached config for source %q channel %q: %w", g.name, channel, e.lastErr)
	}
	// The marker is also the last-use timestamp, including across restarts.
	if err := touchChannelCache(dir); err != nil {
		return nil, err
	}
	return e.version, nil
}

func touchChannelCache(dir string) error {
	now := time.Now()
	if err := os.Chtimes(filepath.Join(dir, "current"), now, now); err != nil {
		return fmt.Errorf("failed to record cache use: %w", err)
	}
	return nil
}

func (g *ChannelGit) loadCached(dir string) (*channelGitVersion, error) {
	b, err := os.ReadFile(filepath.Join(dir, "current"))
	if err != nil {
		return nil, err
	}
	sha := strings.TrimSpace(string(b))
	if len(sha) != 40 || !plumbing.IsHash(sha) {
		return nil, fmt.Errorf("invalid cached revision")
	}
	return g.snapshot(filepath.Join(dir, "tree-"+sha), sha)
}

func (g *ChannelGit) refresh(ctx context.Context, dir string, ref plumbing.ReferenceName, cached *channelGitVersion) (*channelGitVersion, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	// A process interrupted during a clone or publication can leave staging
	// files behind. The channel lock ensures no staging operation is active.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".clone-") || strings.HasPrefix(entry.Name(), ".tree-") || strings.HasPrefix(entry.Name(), ".current-") {
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				return nil, fmt.Errorf("remove abandoned staging files: %w", err)
			}
		}
	}
	auth, err := g.git.auth()
	if err != nil {
		return nil, err
	}
	repoDir := filepath.Join(dir, "repo")
	repo, err := gogit.PlainOpen(repoDir)
	if os.IsNotExist(err) || err == gogit.ErrRepositoryNotExists {
		tmp, err := os.MkdirTemp(dir, ".clone-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		repo, err = gogit.PlainCloneContext(ctx, filepath.Join(tmp, "repo"), true, &gogit.CloneOptions{URL: g.url, Auth: auth, ReferenceName: ref, SingleBranch: true})
		if err != nil {
			return nil, fmt.Errorf("clone: %w", err)
		}
		if err = os.Rename(filepath.Join(tmp, "repo"), repoDir); err != nil {
			return nil, err
		}
		repo, err = gogit.PlainOpen(repoDir)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("open cached repository: %w", err)
	} else {
		err = repo.FetchContext(ctx, &gogit.FetchOptions{Auth: auth, RefSpecs: []gogitconfig.RefSpec{gogitconfig.RefSpec("+" + ref.String() + ":" + ref.String())}, Force: true})
		if err != nil && err != gogit.NoErrAlreadyUpToDate {
			return nil, fmt.Errorf("fetch: %w", err)
		}
	}
	revision, err := repo.Reference(ref, true)
	if err != nil {
		return nil, err
	}
	sha := revision.Hash().String()
	if cached != nil && cached.sha == sha {
		return cached, nil
	}
	commit, err := repo.CommitObject(revision.Hash())
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	// Build a complete checkout before publishing it. A failed fetch or crash
	// leaves the previously published tree and marker untouched.
	tmp, err := os.MkdirTemp(dir, ".tree-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := writeTree(ctx, repo.Storer, tree, tmp); err != nil {
		return nil, err
	}
	v, err := g.snapshot(tmp, sha)
	if err != nil {
		return nil, err
	}
	treeDir := filepath.Join(dir, "tree-"+sha)
	// A tree from an interrupted publication, or a corrupt cache that failed
	// to load, must not replace the fresh tree just built. The active valid
	// tree cannot be this one: the same-SHA case returned above.
	if err := os.RemoveAll(treeDir); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, treeDir); err != nil {
		return nil, err
	}
	marker, err := os.CreateTemp(dir, ".current-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(marker.Name())
	if _, err = marker.WriteString(sha); err != nil {
		marker.Close()
		return nil, err
	}
	if err = marker.Close(); err != nil {
		return nil, err
	}
	if err = os.Rename(marker.Name(), filepath.Join(dir, "current")); err != nil {
		return nil, err
	}
	entries, _ = os.ReadDir(dir)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "tree-") && entry.Name() != "tree-"+sha {
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				log.Warnf("failed to remove old checkout: %v", err)
			}
		}
	}
	return v, nil
}

// snapshot copies files into an independent in-memory filesystem. Symlinks
// are resolved only within the checkout and cycles are rejected.
func (g *ChannelGit) snapshot(root, sha string) (*channelGitVersion, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	files := fstest.MapFS{}
	var copyDir func(string, string, map[string]bool) error
	copyDir = func(dir, rel string, parents map[string]bool) error {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return err
		}
		inside, err := filepath.Rel(root, resolved)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return fmt.Errorf("symlink escapes checkout: %s", rel)
		}
		if parents[resolved] {
			return fmt.Errorf("symlink cycle at %s", rel)
		}
		parents[resolved] = true
		defer delete(parents, resolved)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if rel != "" {
			files[filepath.ToSlash(rel)] = &fstest.MapFile{Mode: fs.ModeDir | 0755}
		}
		for _, entry := range entries {
			name := filepath.Join(dir, entry.Name())
			child := filepath.Join(rel, entry.Name())
			target, err := filepath.EvalSymlinks(name)
			if err != nil {
				return err
			}
			inside, err := filepath.Rel(root, target)
			if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
				return fmt.Errorf("symlink escapes checkout: %s", child)
			}
			info, err := os.Stat(name)
			if err != nil {
				return err
			}
			if info.IsDir() {
				if err := copyDir(name, child, parents); err != nil {
					return err
				}
			} else if info.Mode().IsRegular() {
				data, err := os.ReadFile(name)
				if err != nil {
					return err
				}
				files[filepath.ToSlash(child)] = &fstest.MapFile{Data: data, Mode: info.Mode()}
			}
		}
		return nil
	}
	if err := copyDir(root, "", make(map[string]bool)); err != nil {
		return nil, err
	}
	return &channelGitVersion{sha: sha, config: &SimpleConfig{pathPrefix: g.name + "/", files: files}}, nil
}

// GarbageCollect removes caches that have not been used for maxAge.
func (g *ChannelGit) GarbageCollect(ctx context.Context, _ *log.Entry) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	entries, err := os.ReadDir(g.root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || len(entry.Name()) != 64 {
			continue
		}
		if _, err := hex.DecodeString(entry.Name()); err != nil {
			continue
		}
		e := g.channels[entry.Name()]
		if e != nil && e.users != 0 {
			continue
		}
		dir := filepath.Join(g.root, entry.Name())
		info, err := os.Stat(filepath.Join(dir, "current"))
		if os.IsNotExist(err) {
			info, err = os.Stat(dir)
		}
		if err == nil && time.Since(info.ModTime()) > g.maxAge {
			err = os.RemoveAll(dir)
			if err == nil {
				delete(g.channels, entry.Name())
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}
