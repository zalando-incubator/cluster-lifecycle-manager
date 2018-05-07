package channel

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"path/filepath"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
)

// Git defines a channel source where the channels are stored in a git
// repository.
type Git struct {
	workdir           string
	repositoryURL     string
	repoName          string
	repoDir           string
	sshPrivateKeyFile string
	mutex             *sync.Mutex
}

type staticVersions struct {
	channels map[string]ConfigVersion
}

func NewStaticVersions(versions map[string]ConfigVersion) ConfigVersions {
	return &staticVersions{channels: versions}
}

func (versions *staticVersions) Version(channel string) (ConfigVersion, error) {
	if version, ok := versions.channels[channel]; ok {
		return version, nil
	}
	return "", fmt.Errorf("unknown channel: %s", channel)
}

// NewGit initializes a new git based ChannelSource.
func NewGit(workdir, repositoryURL, sshPrivateKeyFile string) (ConfigSource, error) {
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

// Get checks out the specified version from the git repo.
func (g *Git) Get(version ConfigVersion) (*Config, error) {
	repoDir, err := g.localClone(string(version))
	if err != nil {
		return nil, err
	}

	return &Config{
		Path: repoDir,
	}, nil
}

// Delete deletes the underlying git repository checkout specified by the
// config Path.
func (g *Git) Delete(config *Config) error {
	return os.RemoveAll(config.Path)
}

func (g *Git) Update() (ConfigVersions, error) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	_, err := os.Stat(g.repoDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}

		err = g.cmd("clone", "--mirror", g.repositoryURL, g.repoDir)
		if err != nil {
			return nil, err
		}
	}

	err = g.cmd("--git-dir", g.repoDir, "remote", "update", "--prune")
	if err != nil {
		return nil, err
	}

	return g.availableChannels()
}

func (g *Git) availableChannels() (ConfigVersions, error) {
	cmd := exec.Command("git", "--git-dir", g.repoDir, "show-ref", "--heads")
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	result := make(map[string]ConfigVersion)
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" {
			chunks := strings.Split(line, " ")
			if len(chunks) != 2 {
				return nil, fmt.Errorf("availableChannels: invalid line in show-ref output: %s", line)
			}

			hash := chunks[0]
			channel := strings.Replace(chunks[1], "refs/heads/", "", 1)

			result[channel] = ConfigVersion(hash)
		}
	}
	return &staticVersions{channels: result}, nil
}

// localClone duplicates a repo by cloning to temp location with unix time
// suffix this will be the path that is exposed through the Config. This
// makes sure that each caller (possibly running concurrently) get it's
// own version of the checkout, such that they can run concurrently
// without data races.
func (g *Git) localClone(channel string) (string, error) {
	repoDir := path.Join(g.workdir, fmt.Sprintf("%s_%s_%d", g.repoName, channel, time.Now().UTC().UnixNano()))

	srcRepoUrl := fmt.Sprintf("file://%s", g.repoDir)
	err := g.cmd("clone", srcRepoUrl, repoDir)
	if err != nil {
		return "", err
	}

	err = g.cmd("-C", repoDir, "checkout", channel)
	if err != nil {
		return "", err
	}

	return repoDir, nil
}

// cmd executes a git command with the correct environment set.
func (g *Git) cmd(args ...string) error {
	cmd := exec.Command("git", args...)
	// set GIT_SSH_COMMAND with private-key file when pulling over ssh.
	if g.sshPrivateKeyFile != "" {
		cmd.Env = []string{fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %s -o 'StrictHostKeyChecking no'", g.sshPrivateKeyFile)}
	}

	return command.Run(log.StandardLogger(), cmd)
}
