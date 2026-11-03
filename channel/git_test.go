package channel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/kevinburke/ssh_config"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
)

var testAuthor = &object.Signature{
	Name:  "go-test",
	Email: "go-test@example.com",
	When:  time.Now(),
}

// createGitRepo initialises a bare test repository (simulating the remote)
// with two branches:
//   - main:     stack.yaml contains "channel1"
//   - channel2: stack.yaml contains "channel2"
func createGitRepo(t *testing.T, _ *log.Entry, bareDir string) {
	t.Helper()

	// create a non-bare working repo first, make commits, then push to bare
	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	repo, err := gogit.PlainInitWithOptions(workDir, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	setupExampleConfig(t, workDir, "channel1")
	err = wt.AddGlob("cluster")
	require.NoError(t, err)
	_, err = wt.Commit("initial commit", &gogit.CommitOptions{Author: testAuthor})
	require.NoError(t, err)

	err = wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("channel2"),
		Create: true,
	})
	require.NoError(t, err)

	setupExampleConfig(t, workDir, "channel2")
	err = wt.AddGlob("cluster")
	require.NoError(t, err)
	_, err = wt.Commit("branch commit", &gogit.CommitOptions{Author: testAuthor})
	require.NoError(t, err)

	// create bare repo and push all refs to it
	_, err = gogit.PlainInitWithOptions(bareDir, &gogit.PlainInitOptions{
		Bare:        true,
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	require.NoError(t, err)

	_, err = repo.CreateRemote(&gogitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{bareDir},
	})
	require.NoError(t, err)

	err = repo.Push(&gogit.PushOptions{
		RefSpecs: []gogitconfig.RefSpec{"refs/heads/*:refs/heads/*"},
	})
	require.NoError(t, err)
}

// addCommit adds a new commit on the given branch in the bare repo at bareDir.
func addCommit(t *testing.T, bareDir, branch, stackContent, message string) {
	t.Helper()

	// clone to a temp working dir, commit, push back
	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	repo, err := gogit.PlainClone(workDir, false, &gogit.CloneOptions{
		URL:           bareDir,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
	})
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	setupExampleConfig(t, workDir, stackContent)
	err = wt.AddGlob("cluster")
	require.NoError(t, err)
	_, err = wt.Commit(message, &gogit.CommitOptions{Author: testAuthor})
	require.NoError(t, err)
	err = repo.Push(&gogit.PushOptions{})
	require.NoError(t, err)
}

// deleteBranch removes a branch from the bare repo at bareDir.
func deleteBranch(t *testing.T, bareDir, branch string) {
	t.Helper()

	repo, err := gogit.PlainOpen(bareDir)
	require.NoError(t, err)

	err = repo.Storer.RemoveReference(plumbing.NewBranchReferenceName(branch))
	require.NoError(t, err)
}

// headSHA returns the current SHA of the given branch in the repo at dir.
func headSHA(t *testing.T, dir, branch string) string {
	t.Helper()

	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)

	hash, err := repo.ResolveRevision(plumbing.Revision(branch))
	require.NoError(t, err)

	return hash.String()
}

func checkoutVersion(t *testing.T, logger *log.Entry, source ConfigSource, channel string) Config {
	t.Helper()

	version, err := source.Version(channel, nil)
	require.NoError(t, err)

	cfg, err := version.Get(context.Background(), logger)
	require.NoError(t, err)

	return cfg
}

func TestGitGetFromNonBareLocalSource(t *testing.T) {
	logger := log.WithFields(map[string]interface{}{})

	// non-bare working repo, as used for local development, e.g.
	// --config-source=main:git:file:///path/to/checkout
	repoDir := CreateTempDir(t)
	defer os.RemoveAll(repoDir)

	repo, err := gogit.PlainInitWithOptions(repoDir, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	setupExampleConfig(t, repoDir, "channel1")
	err = wt.AddGlob("cluster")
	require.NoError(t, err)
	_, err = wt.Commit("initial commit", &gogit.CommitOptions{Author: testAuthor})
	require.NoError(t, err)

	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	c, err := NewGit(command.NewExecManager(1), "testsrc", workDir, "file://"+repoDir, "")
	require.NoError(t, err)

	err = c.Update(context.Background(), logger)
	require.NoError(t, err)

	main := checkoutVersion(t, logger, c, "main")
	verifyExampleConfig(t, main, "testsrc", "channel1")
}

// TestGitGetFromNonBareLocalSourceWithTrackedConfigDir ensures a tracked
// "config" file or directory at the root of a non-bare checkout isn't
// misclassified as a bare repository — only ".git/config" should be used
// to detect a bare repo.
func TestGitGetFromNonBareLocalSourceWithTrackedConfigDir(t *testing.T) {
	logger := log.WithFields(map[string]interface{}{})

	repoDir := CreateTempDir(t)
	defer os.RemoveAll(repoDir)

	repo, err := gogit.PlainInitWithOptions(repoDir, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	setupExampleConfig(t, repoDir, "channel1")
	SetupConfig(t, repoDir, map[string]string{"config/app.yaml": "tracked-config-dir"})
	err = wt.AddGlob("cluster")
	require.NoError(t, err)
	err = wt.AddGlob("config")
	require.NoError(t, err)
	_, err = wt.Commit("initial commit", &gogit.CommitOptions{Author: testAuthor})
	require.NoError(t, err)

	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	c, err := NewGit(command.NewExecManager(1), "testsrc", workDir, "file://"+repoDir, "")
	require.NoError(t, err)

	err = c.Update(context.Background(), logger)
	require.NoError(t, err)

	main := checkoutVersion(t, logger, c, "main")
	verifyExampleConfig(t, main, "testsrc", "channel1")
}

func TestGitGet(t *testing.T) {
	logger := log.WithFields(map[string]interface{}{})

	repoTempDir := CreateTempDir(t)
	defer os.RemoveAll(repoTempDir)

	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	createGitRepo(t, logger, repoTempDir)
	c, err := NewGit(command.NewExecManager(1), "testsrc", workDir, repoTempDir, "")
	require.NoError(t, err)

	err = c.Update(context.Background(), logger)
	require.NoError(t, err)

	// check main channel
	main := checkoutVersion(t, logger, c, "main")
	verifyExampleConfig(t, main, "testsrc", "channel1")

	// check another channel
	channel2 := checkoutVersion(t, logger, c, "channel2")
	verifyExampleConfig(t, channel2, "testsrc", "channel2")

	// check checkout by SHA
	sha := headSHA(t, repoTempDir, "main")
	bySHA := checkoutVersion(t, logger, c, sha)
	verifyExampleConfig(t, bySHA, "testsrc", "channel1")
}

func TestGitUpdateRepeat(t *testing.T) {
	logger := log.WithFields(map[string]interface{}{})

	repoTempDir := CreateTempDir(t)
	defer os.RemoveAll(repoTempDir)

	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	createGitRepo(t, logger, repoTempDir)
	c, err := NewGit(command.NewExecManager(1), "testsrc", workDir, repoTempDir, "")
	require.NoError(t, err)

	err = c.Update(context.Background(), logger)
	require.NoError(t, err)

	v1, err := c.Version("main", nil)
	require.NoError(t, err)
	sha1 := v1.ID()

	// add a new commit to main on the remote
	addCommit(t, repoTempDir, "main", "channel1-v2", "second commit")

	// Update should pick up the new commit
	err = c.Update(context.Background(), logger)
	require.NoError(t, err)

	v2, err := c.Version("main", nil)
	require.NoError(t, err)
	require.NotEqual(t, sha1, v2.ID(), "SHA should advance after second commit")

	cfg := checkoutVersion(t, logger, c, v2.ID())
	verifyExampleConfig(t, cfg, "testsrc", "channel1-v2")

	// deleted branch should no longer resolve after pruning update
	deleteBranch(t, repoTempDir, "channel2")

	err = c.Update(context.Background(), logger)
	require.NoError(t, err)

	_, err = c.Version("channel2", nil)
	require.Error(t, err, "deleted branch should not resolve after pruning update")
}

func TestGitStaleWorkdirCleanup(t *testing.T) {
	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	// create stale dirs that NewGit should clean up
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "kubernetes-on-aws_abc123_1234_1"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "kubernetes-on-aws_def456_5678_2"), 0755))
	// different repo name — should be left alone
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "other-repo_abc123_1234_1"), 0755))

	_, err := NewGit(command.NewExecManager(1), "test", workDir,
		"https://github.com/zalando-incubator/kubernetes-on-aws.git", "")
	require.NoError(t, err)

	entries, _ := os.ReadDir(workDir)
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), "kubernetes-on-aws_"),
			"stale dir should be removed: %s", e.Name())
	}
	_, err = os.Stat(filepath.Join(workDir, "other-repo_abc123_1234_1"))
	require.NoError(t, err, "unrelated dir should survive")
}

func TestGitSSHIdentityFile(t *testing.T) {
	sshConfig, err := ssh_config.Decode(strings.NewReader(`
Host ghe-logging.nonexistent.zalan.do
  HostName github.com
  IdentityFile ~/.ssh/id_logging
`))
	require.NoError(t, err)

	g := &Git{
		repositoryURL: "git@ghe-logging.nonexistent.zalan.do:zalando-build/kubernetes-logging-manifests.git",
		sshConfig:     sshConfig,
	}

	// per-host IdentityFile should be used instead of a global key
	// (HostName aliasing is handled natively by go-git's SSH transport)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".ssh", "id_logging"), g.sshIdentityFile())
}

func TestGitSSHIdentityFileQuoted(t *testing.T) {
	sshConfig, err := ssh_config.Decode(strings.NewReader(`
Host ghe-logging.nonexistent.zalan.do
  HostName github.com
  IdentityFile "~/.ssh/id_logging"
`))
	require.NoError(t, err)

	g := &Git{
		repositoryURL: "git@ghe-logging.nonexistent.zalan.do:zalando-build/kubernetes-logging-manifests.git",
		sshConfig:     sshConfig,
	}

	// surrounding quotes, as allowed by the ssh_config spec, must be
	// stripped before "~/" expansion
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".ssh", "id_logging"), g.sshIdentityFile())
}

func TestGitSSHIdentityFileNoConfigMatch(t *testing.T) {
	sshConfig, err := ssh_config.Decode(strings.NewReader(`
Host some-other-alias
  HostName example.com
`))
	require.NoError(t, err)

	g := &Git{
		repositoryURL: "git@github.com:zalando-incubator/kubernetes-on-aws.git",
		sshConfig:     sshConfig,
	}

	// no matching Host block — auth falls back to defaults
	require.Empty(t, g.sshIdentityFile())
}

func TestNewGitSkipsSSHConfigForNonSSHSources(t *testing.T) {
	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	c, err := NewGit(command.NewExecManager(1), "test", workDir,
		"https://github.com/zalando-incubator/kubernetes-on-aws.git", "")
	require.NoError(t, err)

	g, ok := c.(*Git)
	require.True(t, ok)
	// ~/.ssh/config is never parsed for HTTPS sources, so a missing or
	// malformed file can't block them from starting
	require.Nil(t, g.sshConfig)
}

// TestNewGitToleratesUnparsableSSHConfig ensures an SSH source doesn't fail
// to start when ~/.ssh/config contains constructs the ssh_config library
// can't parse (e.g. a Match directive) — it should fall back to
// --ssh-private-key-path instead.
func TestNewGitToleratesUnparsableSSHConfig(t *testing.T) {
	home := CreateTempDir(t)
	defer os.RemoveAll(home)

	SetupConfig(t, home, map[string]string{
		".ssh/config": "Match host ghe-logging.nonexistent.zalan.do\n  IdentityFile ~/.ssh/id_logging\n",
	})
	t.Setenv("HOME", home)

	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	c, err := NewGit(command.NewExecManager(1), "test", workDir,
		"git@ghe-logging.nonexistent.zalan.do:zalando-build/kubernetes-logging-manifests.git", "/fallback/key")
	require.NoError(t, err)

	g, ok := c.(*Git)
	require.True(t, ok)
	require.Nil(t, g.sshConfig)
	require.Empty(t, g.sshIdentityFile())
}

func TestGitLocalCloneExecutableAndSymlink(t *testing.T) {
	logger := log.WithFields(map[string]interface{}{})

	repoDir := CreateTempDir(t)
	defer os.RemoveAll(repoDir)

	repo, err := gogit.PlainInitWithOptions(repoDir, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	setupExampleConfig(t, repoDir, "channel1")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "cluster", "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0755))
	require.NoError(t, os.Symlink("run.sh", filepath.Join(repoDir, "cluster", "run-link.sh")))

	err = wt.AddGlob("cluster")
	require.NoError(t, err)
	_, err = wt.Commit("initial commit", &gogit.CommitOptions{Author: testAuthor})
	require.NoError(t, err)

	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	c, err := NewGit(command.NewExecManager(1), "testsrc", workDir, "file://"+repoDir, "")
	require.NoError(t, err)

	err = c.Update(context.Background(), logger)
	require.NoError(t, err)

	version, err := c.Version("main", nil)
	require.NoError(t, err)
	cfg, err := version.Get(context.Background(), logger)
	require.NoError(t, err)

	simpleCfg, ok := cfg.(*SimpleConfig)
	require.True(t, ok)

	scriptPath := filepath.Join(simpleCfg.baseDir, "cluster", "run.sh")
	info, err := os.Stat(scriptPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm())

	linkPath := filepath.Join(simpleCfg.baseDir, "cluster", "run-link.sh")
	linkInfo, err := os.Lstat(linkPath)
	require.NoError(t, err)
	require.True(t, linkInfo.Mode()&os.ModeSymlink != 0, "expected run-link.sh to be a symlink")

	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	require.Equal(t, "run.sh", target)
}

// TestSafeMkdirAllRejectsSymlinkEscape ensures writeTree's directory-creation
// helper refuses to descend through a path component that already exists
// but is not a directory — in particular, a symlink written for an earlier
// tree entry pointing outside the checkout. Without this guard, a crafted
// tree with a symlink entry (e.g. "redirect") followed by a nested entry
// using that name as a directory component (e.g. "redirect/evil.yaml")
// would let MkdirAll/OpenFile follow the symlink and write outside dir.
func TestSafeMkdirAllRejectsSymlinkEscape(t *testing.T) {
	root := CreateTempDir(t)
	defer os.RemoveAll(root)

	outsideDir := CreateTempDir(t)
	defer os.RemoveAll(outsideDir)

	require.NoError(t, os.Symlink(outsideDir, filepath.Join(root, "redirect")))

	err := safeMkdirAll(root, filepath.Join(root, "redirect", "nested"))
	require.Error(t, err, "must refuse to descend through a symlink")

	_, statErr := os.Stat(filepath.Join(outsideDir, "nested"))
	require.True(t, os.IsNotExist(statErr), "nested dir must not have been created outside the checkout")
}

func TestSafeJoinRejectsPathEscape(t *testing.T) {
	_, err := safeJoin("/checkout", "../../etc/passwd")
	require.Error(t, err)
}

func TestGitLocalCloneConcurrentIndependence(t *testing.T) {
	logger := log.WithFields(map[string]interface{}{})

	repoTempDir := CreateTempDir(t)
	defer os.RemoveAll(repoTempDir)

	workDir := CreateTempDir(t)
	defer os.RemoveAll(workDir)

	createGitRepo(t, logger, repoTempDir)
	c, err := NewGit(command.NewExecManager(1), "testsrc", workDir, repoTempDir, "")
	require.NoError(t, err)

	err = c.Update(context.Background(), logger)
	require.NoError(t, err)

	const workers = 10
	results := make([]Config, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			channel := "main"
			if i%2 == 0 {
				channel = "channel2"
			}
			version, err := c.Version(channel, nil)
			if err != nil {
				errs[i] = err
				return
			}
			results[i], errs[i] = version.Get(context.Background(), logger)
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		require.NoError(t, errs[i])
		if i%2 == 0 {
			verifyExampleConfig(t, results[i], "testsrc", "channel2")
		} else {
			verifyExampleConfig(t, results[i], "testsrc", "channel1")
		}
	}
}

func TestGetRepoName(t *testing.T) {
	for _, tc := range []struct {
		msg     string
		name    string
		uri     string
		success bool
	}{
		{
			msg:     "get reponame from github URL",
			name:    "kubernetes-on-aws",
			uri:     "https://github.com/zalando-incubator/kubernetes-on-aws.git",
			success: true,
		},
		{
			msg:     "get reponame from full local path with .git suffix",
			name:    "kubernetes-on-aws",
			uri:     "/kubernetes-on-aws.git",
			success: true,
		},
		{
			msg:     "get reponame from relative local path with .git suffix",
			name:    "kubernetes-on-aws",
			uri:     "kubernetes-on-aws.git",
			success: true,
		},
		{
			msg:     "get reponame from dot relative local path with .git suffix",
			name:    "kubernetes-on-aws",
			uri:     "./kubernetes-on-aws.git",
			success: true,
		},
		{
			msg:     "get reponame from full local path without .git suffix",
			name:    "kubernetes-on-aws",
			uri:     "/kubernetes-on-aws",
			success: true,
		},
		{
			msg:     "get reponame from relative local path without .git suffix",
			name:    "kubernetes-on-aws",
			uri:     "kubernetes-on-aws",
			success: true,
		},
		{
			msg:     "get reponame from dot relative local path without .git suffix",
			name:    "kubernetes-on-aws",
			uri:     "./kubernetes-on-aws",
			success: true,
		},
		{
			msg:     "empty relative path should be invalid",
			name:    "",
			uri:     "./",
			success: false,
		},
		{
			msg:     "empty full path should be invalid",
			name:    "",
			uri:     "/",
			success: false,
		},
		{
			msg:     "empty uri should be invalid",
			name:    "",
			uri:     "",
			success: false,
		},
	} {
		t.Run(tc.msg, func(t *testing.T) {
			name, err := getRepoName(tc.uri)
			if err != nil && tc.success {
				t.Errorf("should not fail: %s", err)
			}

			if name != tc.name && tc.success {
				t.Errorf("expected repo name %s, got %s", tc.name, name)
			}
		})
	}
}
