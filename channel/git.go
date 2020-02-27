package channel

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
)

// Git defines a channel source where the channels are stored in a git
// repository.
type Git struct {
	name              string
	exec              *command.ExecManager
	workdir           string
	repositoryURL     string
	repoName          string
	repoDir           string
	sshPrivateKeyFile string
	mutex             *sync.Mutex
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
func NewGit(execManager *command.ExecManager, name string, workdir, repositoryURL, sshPrivateKeyFile string) (ConfigSource, error) {
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return nil, err
	}

	// get repo name from repo URL.
	repoName, err := getRepoName(repositoryURL)
	if err != nil {
		return nil, err
	}

	return &Git{
		name:              name,
		exec:              execManager,
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

	_, err := os.Stat(g.repoDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}

		err = g.cmd(ctx, logger, "clone", "--mirror", g.repositoryURL, g.repoDir)
		if err != nil {
			return err
		}
	}

	err = g.cmd(ctx, logger, "--git-dir", g.repoDir, "remote", "update", "--prune")
	if err != nil {
		return err
	}

	return nil
}

func (g *Git) Version(channel string, overrides map[string]string) (ConfigVersion, error) {
	sha, err := exec.Command("git", "--git-dir", g.repoDir, "rev-parse", channel).Output()
	if err != nil {
		return nil, err
	}

	return &gitVersion{
		git: g,
		sha: strings.TrimSpace(string(sha)),
	}, nil
}

// localClone duplicates a repo by cloning to temp location with unix time
// suffix this will be the path that is exposed through the Config. This
// makes sure that each caller (possibly running concurrently) get it's
// own version of the checkout, such that they can run concurrently
// without data races.
func (g *Git) localClone(ctx context.Context, logger *log.Entry, channel string) (string, error) {
	repoDir := path.Join(g.workdir, fmt.Sprintf("%s_%s_%d", g.repoName, channel, time.Now().UTC().UnixNano()))

	srcRepoUrl := fmt.Sprintf("file://%s", g.repoDir)
	err := g.cmd(ctx, logger, "clone", srcRepoUrl, repoDir)
	if err != nil {
		return "", err
	}

	err = g.cmd(ctx, logger, "-C", repoDir, "checkout", channel)
	if err != nil {
		return "", err
	}

	return repoDir, nil
}

// cmd executes a git command with the correct environment set.
func (g *Git) cmd(ctx context.Context, logger *log.Entry, args ...string) error {
	cmd := exec.Command("git", args...)
	// set GIT_SSH_COMMAND with private-key file when pulling over ssh.
	if g.sshPrivateKeyFile != "" {
		cmd.Env = []string{fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %s -o 'StrictHostKeyChecking no'", g.sshPrivateKeyFile)}
	}

	_, err := g.exec.RunSilently(ctx, logger, cmd)
	return err
}
