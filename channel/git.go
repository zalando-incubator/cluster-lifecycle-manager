package channel

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/kevinburke/ssh_config"
	log "github.com/sirupsen/logrus"
	stdssh "golang.org/x/crypto/ssh"
)

func init() {
	// Replace the default file:// transport (which shells out to git-upload-pack)
	// with an in-process server-based transport, so local clones from the
	// mirror — bare or non-bare — work without the git binary in the
	// container image.
	client.InstallProtocol("file", server.NewClient(&localFSLoader{base: osfs.New("")}))
}

// localFSLoader is a server.Loader for local file:// endpoints. It supports
// both bare mirrors (used internally for per-cluster checkouts) and
// non-bare working repositories (useful for local development against a
// git checkout on disk, e.g. --config-source=main:git:file:///path/to/repo).
//
// server.DefaultLoader only handles this correctly for bare repos: when it
// finds a ".git" subdirectory it still roots the storage at the repo root
// instead of inside ".git", which go-git's packfile negotiation reports as
// "remote repository is empty". We chroot into ".git" ourselves for
// non-bare repos before handing off to go-git's storage layer. ".git/config"
// is checked before a root-level "config" so a tracked file or directory
// named "config" in a non-bare checkout isn't misclassified as bare.
type localFSLoader struct {
	base billy.Filesystem
}

func (l *localFSLoader) Load(ep *transport.Endpoint) (storer.Storer, error) {
	fs, err := l.base.Chroot(ep.Path)
	if err != nil {
		return nil, err
	}

	// check .git/config first: a non-bare checkout can have its own
	// tracked file or directory named "config" at the repo root, which
	// would otherwise be misclassified as a bare repository and leave
	// storage rooted outside the actual objects/refs.
	if gitDir, err := fs.Chroot(gogit.GitDirName); err == nil {
		if _, err := gitDir.Stat("config"); err == nil {
			return filesystem.NewStorage(gitDir, cache.NewObjectLRUDefault()), nil
		}
	}

	if _, err := fs.Stat("config"); err == nil {
		// bare repository — objects/refs live at the root
		return filesystem.NewStorage(fs, cache.NewObjectLRUDefault()), nil
	}

	return nil, transport.ErrRepositoryNotFound
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
	sshConfig         *ssh_config.Config
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
func NewGit(name string, workdir, repositoryURL, sshPrivateKeyFile string) (ConfigSource, error) {
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

	// only parse ~/.ssh/config for SSH sources — an unreadable or malformed
	// file would otherwise prevent unrelated HTTPS/local-file sources from
	// starting. A parse failure for an SSH source (e.g. an unsupported
	// Match directive) falls back to --ssh-private-key-path rather than
	// failing NewGit outright.
	var sshCfg *ssh_config.Config
	if isSSHURL(repositoryURL) {
		sshCfg, err = loadSSHConfig()
		if err != nil {
			log.Warnf("failed to load SSH config for %q, falling back to --ssh-private-key-path: %v", name, err)
		}
	}

	return &Git{
		name:              name,
		workdir:           absWorkdir,
		repoName:          repoName,
		repositoryURL:     repositoryURL,
		repoDir:           path.Join(absWorkdir, repoName),
		sshPrivateKeyFile: sshPrivateKeyFile,
		sshConfig:         sshCfg,
		mutex:             &sync.Mutex{},
	}, nil
}

// loadSSHConfig parses the user's ~/.ssh/config, if present. This mirrors
// the behaviour of the real ssh binary used before the go-git migration,
// which resolves host aliases (HostName) and per-host private keys
// (IdentityFile) from this file — used for example when multiple git
// remotes are configured under aliased hosts, each with its own key.
func loadSSHConfig() (*ssh_config.Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}

	f, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	return ssh_config.Decode(f)
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

	// mirror repo already exists (persistent volume from a previous run) — open and fetch
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
	// local clones don't fail if the default branch was renamed; if the
	// sync fails, verify that HEAD still resolves — a dangling HEAD after
	// a prune would cause all subsequent local clones to fail.
	if syncErr := g.syncMirrorHEAD(repo); syncErr != nil {
		logger.Warnf("failed to sync mirror HEAD: %v", syncErr)
		if _, resolveErr := repo.ResolveRevision(plumbing.Revision(plumbing.HEAD)); resolveErr != nil {
			return fmt.Errorf("mirror HEAD does not resolve after failed sync: %w", resolveErr)
		}
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

// localClone writes out the files of the tree at the given SHA into a new
// directory. Each call gets its own directory so concurrent callers don't
// interfere with each other.
//
// This reads blobs directly from the mirror's object store and streams them
// to disk one at a time (via object.File.Reader), instead of creating a
// second go-git Repository/Worktree and running a full clone + checkout.
// With hundreds of concurrent callers, decoding and holding full working
// trees as go-git's in-memory object graph (per concurrent clone) caused
// unbounded memory growth; streaming individual blobs keeps each caller's
// memory footprint proportional to one file at a time, not the whole tree.
//
// The mirror is reopened here with a small, independent object cache
// (rather than reusing g's shared *gogit.Repository, which defaults to a
// 96MiB LRU cache) so concurrent calls don't each retain tens of megabytes
// of decoded blobs — with 200 concurrent callers the default cache alone
// could retain multiple gigabytes.
func (g *Git) localClone(ctx context.Context, _ *log.Entry, sha string) (string, error) {
	i := atomic.AddUint64(&g.counter, 1)
	repoDir := path.Join(g.workdir, fmt.Sprintf("%s_%s_%d_%d", g.repoName, sha, time.Now().UTC().UnixNano(), i))

	storer := filesystem.NewStorage(osfs.New(g.repoDir), cache.NewObjectLRU(1*cache.MiByte))
	repo, err := gogit.Open(storer, nil)
	if err != nil {
		return "", fmt.Errorf("failed to open mirror repo: %w", err)
	}

	commit, err := repo.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return "", fmt.Errorf("failed to resolve commit %s: %w", sha, err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return "", fmt.Errorf("failed to get tree for %s: %w", sha, err)
	}

	if err := writeTree(ctx, storer, tree, repoDir); err != nil {
		os.RemoveAll(repoDir)
		return "", fmt.Errorf("failed to write tree for %s: %w", sha, err)
	}

	return repoDir, nil
}

// writeTree streams every file in tree to disk under dir, one blob at a
// time, creating parent directories as needed. Symlinks are recreated as
// symlinks; submodules are skipped since they have no checked-out content
// to write.
//
// This walks the tree manually instead of using object.TreeWalker: in
// go-git v5.19.3, TreeWalker.Next() reports io.EOF when loading a child
// tree fails (see GetTree error handling in tree.go), which a caller
// treating io.EOF as "done" would silently treat as a successfully
// completed, but partial, checkout.
//
// Every destination path is required to resolve to a location inside dir.
// Without this, a crafted tree containing a symlink entry (e.g. "link")
// followed by a nested entry using that name as a directory component
// (e.g. "link/file.yaml") would cause MkdirAll/OpenFile to follow the
// symlink and write outside the checkout.
func writeTree(ctx context.Context, storer storer.EncodedObjectStorer, tree *object.Tree, dir string) error {
	return writeTreeEntries(ctx, storer, tree, "", dir)
}

func writeTreeEntries(ctx context.Context, storer storer.EncodedObjectStorer, tree *object.Tree, base, dir string) error {
	for _, entry := range tree.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}

		name := entry.Name
		if base != "" {
			name = base + "/" + entry.Name
		}

		if entry.Mode == filemode.Submodule {
			continue
		}

		if entry.Mode == filemode.Dir {
			subtree, err := object.GetTree(storer, entry.Hash)
			if err != nil {
				return fmt.Errorf("failed to get subtree %s: %w", name, err)
			}
			if err := writeTreeEntries(ctx, storer, subtree, name, dir); err != nil {
				return err
			}
			continue
		}

		dest, err := safeJoin(dir, name)
		if err != nil {
			return err
		}

		if err := safeMkdirAll(dir, filepath.Dir(dest)); err != nil {
			return err
		}

		blob, err := object.GetBlob(storer, entry.Hash)
		if err != nil {
			return fmt.Errorf("failed to get blob for %s: %w", name, err)
		}

		if entry.Mode == filemode.Symlink {
			if err := writeSymlink(blob, dest); err != nil {
				return err
			}
			continue
		}

		if err := writeBlob(blob, dest, entry.Mode); err != nil {
			return err
		}
	}

	return nil
}

// safeJoin joins dir and name and verifies the result is still located
// inside dir, rejecting any ".."-escaping path.
func safeJoin(dir, name string) (string, error) {
	dest := filepath.Join(dir, name)
	rel, err := filepath.Rel(dir, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("entry %q escapes checkout directory", name)
	}
	return dest, nil
}

// safeMkdirAll creates destDir and any missing parents under root, refusing
// to descend through any path component that already exists but is not a
// directory (in particular, a symlink written for an earlier tree entry).
// This prevents a later entry from following a symlink out of the checkout.
func safeMkdirAll(root, destDir string) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}

	rel, err := filepath.Rel(root, destDir)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}

	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				if err := os.Mkdir(current, 0755); err != nil {
					return err
				}
				continue
			}
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("refusing to write through non-directory %q", current)
		}
	}

	return nil
}

func writeBlob(blob *object.Blob, dest string, mode filemode.FileMode) error {
	reader, err := blob.Reader()
	if err != nil {
		return err
	}
	defer reader.Close()

	perm := os.FileMode(0644)
	if mode == filemode.Executable {
		perm = 0755
	}

	// O_EXCL rejects an existing destination outright, rather than
	// following/truncating it. Each checkout directory is freshly created,
	// so the only way dest could already exist is a duplicate tree entry
	// name (e.g. a symlink and a regular file sharing the same path) —
	// which would otherwise let a later entry overwrite whatever an
	// earlier symlink entry pointed at, including locations outside dir.
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", dest, err)
	}

	if _, err := io.Copy(f, reader); err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

func writeSymlink(blob *object.Blob, dest string) error {
	reader, err := blob.Reader()
	if err != nil {
		return err
	}
	defer reader.Close()

	target, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	// dest must not already exist, for the same reason as writeBlob: each
	// checkout directory is fresh, so an existing path here means a
	// duplicate tree entry name that could otherwise be used to retarget
	// or escape through a previously written symlink.
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("refusing to overwrite existing entry %q", dest)
	} else if !os.IsNotExist(err) {
		return err
	}

	return os.Symlink(string(target), dest)
}

// auth returns SSH auth when the repository URL is an SSH endpoint, nil otherwise.
// This avoids passing SSH credentials to HTTPS transports which reject them.
func (g *Git) auth() (transport.AuthMethod, error) {
	if !isSSHURL(g.repositoryURL) {
		return nil, nil
	}

	username := sshUsername(g.repositoryURL)
	keyFile := g.sshPrivateKeyFile
	if identityFile := g.sshIdentityFile(); identityFile != "" {
		keyFile = identityFile
	}
	if keyFile == "" {
		return nil, nil
	}

	auth, err := gogitssh.NewPublicKeysFromFile(username, keyFile, "")
	if err != nil {
		return nil, fmt.Errorf("failed to load SSH key %s: %w", keyFile, err)
	}
	// mirror the original behaviour: StrictHostKeyChecking no
	auth.HostKeyCallback = stdssh.InsecureIgnoreHostKey()
	return auth, nil
}

// sshIdentityFile resolves a per-host IdentityFile from the user's
// ~/.ssh/config for the repository's host alias, mirroring the real ssh
// binary's behaviour used before the go-git migration (e.g. one key per
// aliased host, as configured for multiple git remotes).
func (g *Git) sshIdentityFile() string {
	if g.sshConfig == nil {
		return ""
	}

	ep, err := transport.NewEndpoint(g.repositoryURL)
	if err != nil {
		return ""
	}

	identityFile, err := g.sshConfig.Get(ep.Host, "IdentityFile")
	if err != nil || identityFile == "" {
		return ""
	}
	identityFile = strings.Trim(identityFile, `"`)

	home, homeErr := os.UserHomeDir()
	if homeErr == nil && strings.HasPrefix(identityFile, "~/") {
		identityFile = filepath.Join(home, identityFile[2:])
	}

	return identityFile
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
