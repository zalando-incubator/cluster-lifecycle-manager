package channel

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	stdssh "golang.org/x/crypto/ssh"
)

func init() {
	// Replace the default file:// transport (which shells out to git-upload-pack)
	// with an in-process server-based transport, so local clones from the bare
	// mirror work without the git binary present in the container image.
	// This only affects local bare repos (g.repoDir) — the initial remote clone
	// always uses HTTPS or SSH in production.
	client.InstallProtocol("file", server.NewClient(server.DefaultLoader))
}

// Git defines a channel source where the channels are stored in a git
// repository.
type Git struct {
	name              string
	workdir           string
	repositoryURL     string
	repoName          string
	repoDir           string
	sshPrivateKeyFile string
	mutex             *sync.Mutex
	counter           uint64
}

type gitVersion struct {
	git *Git
	sha string
}

func (v *gitVersion) ID() string {
	return v.sha
}

func (v *gitVersion) Get(ctx context.Context, logger *log.Entry) (Config, error) {
	repoDir, err := v.git.localClone(ctx, logger, v.sha)
	if err != nil {
		return nil, err
	}

	return NewSimpleConfig(v.git.name, repoDir, true)
}

// NewGit initializes a new git based ChannelSource.
func NewGit(_ *command.ExecManager, name string, workdir, repositoryURL, sshPrivateKeyFile string) (ConfigSource, error) {
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return nil, err
	}

	repoName, err := getRepoName(repositoryURL)
	if err != nil {
		return nil, err
	}

	entries, _ := os.ReadDir(absWorkdir)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), repoName+"_") {
			os.RemoveAll(path.Join(absWorkdir, e.Name()))
		}
	}

	return &Git{
		name:              name,
		workdir:           absWorkdir,
		repoName:          repoName,
		repositoryURL:     repositoryURL,
		repoDir:           path.Join(absWorkdir, repoName),
		sshPrivateKeyFile: sshPrivateKeyFile,
		mutex:             &sync.Mutex{},
	}, nil
}

var repoNameRE = regexp.MustCompile(`/?([\w-]+)(.git)?$`)

// getRepoName parses the repository name given a repository URI.
func getRepoName(repoURI string) (string, error) {
	match := repoNameRE.FindStringSubmatch(repoURI)
	if len(match) != 3 {
		return "", fmt.Errorf("could not parse repository name from uri: %s", repoURI)
	}
	return match[1], nil
}

func (g *Git) Name() string {
	return g.name
}

func (g *Git) Update(ctx context.Context, logger *log.Entry) error {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	auth, err := g.auth()
	if err != nil {
		return err
	}

	_, err = os.Stat(g.repoDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}

		// bare mirror clone — fetches all refs; log name only, not URL (may contain credentials)
		logger.Infof("cloning source %q into %s", g.name, g.repoDir)
		_, err = gogit.PlainCloneContext(ctx, g.repoDir, true, &gogit.CloneOptions{
			URL:    g.repositoryURL,
			Auth:   auth,
			Mirror: true,
		})
		if err != nil {
			return fmt.Errorf("failed to clone %q: %w", g.name, err)
		}
		return nil
	}

	repo, err := gogit.PlainOpen(g.repoDir)
	if err != nil {
		return fmt.Errorf("failed to open mirror repo: %w", err)
	}

	// fetch all refs, pruning deleted remote refs
	err = repo.FetchContext(ctx, &gogit.FetchOptions{
		Auth:     auth,
		RefSpecs: []gogitconfig.RefSpec{"refs/*:refs/*"},
		Prune:    true,
		Force:    true,
	})
	if err != nil && err != gogit.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to fetch %q: %w", g.name, err)
	}

	// sync mirror HEAD to the remote's current default branch so that
	// local clones don't fail if the default branch was renamed
	if syncErr := g.syncMirrorHEAD(repo); syncErr != nil {
		logger.Warnf("failed to sync mirror HEAD (non-fatal): %v", syncErr)
	}

	// light repack equivalent to gc --prune=now
	if err := repo.RepackObjects(&gogit.RepackConfig{UseRefDeltas: true}); err != nil {
		logger.Warnf("repack after fetch failed (non-fatal): %v", err)
	}

	return nil
}

// syncMirrorHEAD updates the mirror's symbolic HEAD to match the remote's
// advertised default branch, preventing stale HEAD after branch renames.
func (g *Git) syncMirrorHEAD(repo *gogit.Repository) error {
	auth, err := g.auth()
	if err != nil {
		return err
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		return err
	}

	refs, err := remote.List(&gogit.ListOptions{Auth: auth})
	if err != nil {
		return err
	}

	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD && ref.Type() == plumbing.SymbolicReference {
			return repo.Storer.SetReference(
				plumbing.NewSymbolicReference(plumbing.HEAD, ref.Target()),
			)
		}
	}

	return nil
}

// GarbageCollect performs garbage collection on the git repository.
func (g *Git) GarbageCollect(_ context.Context, logger *log.Entry) error {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	_, err := os.Stat(g.repoDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	repo, err := gogit.PlainOpen(g.repoDir)
	if err != nil {
		return fmt.Errorf("failed to open mirror repo for gc: %w", err)
	}

	// aggressive repack equivalent to gc --aggressive
	if err := repo.RepackObjects(&gogit.RepackConfig{UseRefDeltas: true}); err != nil {
		logger.Warnf("aggressive repack failed (non-fatal): %v", err)
	}

	return nil
}

func (g *Git) Version(channel string, _ map[string]string) (ConfigVersion, error) {
	repo, err := gogit.PlainOpen(g.repoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open mirror repo: %w", err)
	}

	hash, err := repo.ResolveRevision(plumbing.Revision(channel))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %q: %w", channel, err)
	}

	return &gitVersion{
		git: g,
		sha: hash.String(),
	}, nil
}

// localClone creates an independent local clone of the mirror repo checked out
// at the given SHA. Each call gets its own directory so concurrent callers
// don't interfere with each other. The mirror path is passed as a bare local
// path (not file://) so go-git uses its in-process filesystem transport,
// avoiding any dependency on the git binary.
func (g *Git) localClone(ctx context.Context, _ *log.Entry, sha string) (string, error) {
	i := atomic.AddUint64(&g.counter, 1)
	repoDir := path.Join(g.workdir, fmt.Sprintf("%s_%s_%d_%d", g.repoName, sha, time.Now().UTC().UnixNano(), i))

	// use the bare local path — go-git detects local endpoints and uses
	// in-process filesystem storage, no git binary required
	_, err := gogit.PlainCloneContext(ctx, repoDir, false, &gogit.CloneOptions{
		URL:               g.repoDir,
		NoCheckout:        true,
		RecurseSubmodules: gogit.NoRecurseSubmodules,
	})
	if err != nil {
		return "", fmt.Errorf("failed to local clone: %w", err)
	}

	repo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		os.RemoveAll(repoDir)
		return "", fmt.Errorf("failed to open local clone: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		os.RemoveAll(repoDir)
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	err = wt.Checkout(&gogit.CheckoutOptions{
		Hash:  plumbing.NewHash(sha),
		Force: true,
	})
	if err != nil {
		os.RemoveAll(repoDir)
		return "", fmt.Errorf("failed to checkout %s: %w", sha, err)
	}

	return repoDir, nil
}

// auth returns SSH auth when the repository URL is an SSH endpoint, nil otherwise.
// This avoids passing SSH credentials to HTTPS transports which reject them.
func (g *Git) auth() (transport.AuthMethod, error) {
	if g.sshPrivateKeyFile == "" || !isSSHURL(g.repositoryURL) {
		return nil, nil
	}

	username := sshUsername(g.repositoryURL)
	auth, err := gogitssh.NewPublicKeysFromFile(username, g.sshPrivateKeyFile, "")
	if err != nil {
		return nil, fmt.Errorf("failed to load SSH key %s: %w", g.sshPrivateKeyFile, err)
	}
	// mirror the original behaviour: StrictHostKeyChecking no
	auth.HostKeyCallback = stdssh.InsecureIgnoreHostKey()
	return auth, nil
}

// isSSHURL reports whether the given URL uses SSH transport by parsing it
// with go-git's endpoint parser, which handles ssh://, SCP-style (git@host:path),
// and SCP-style without a username (host:path).
func isSSHURL(rawURL string) bool {
	ep, err := transport.NewEndpoint(rawURL)
	if err != nil {
		return false
	}
	return ep.Protocol == "ssh"
}

// sshUsername extracts the username from the repository URL, defaulting to "git".
func sshUsername(rawURL string) string {
	ep, err := transport.NewEndpoint(rawURL)
	if err == nil && ep.User != "" {
		return ep.User
	}
	return "git"
}
