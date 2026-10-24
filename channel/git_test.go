package channel

import (
	"context"
	"os"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
)

var testAuthor = &object.Signature{
	Name:  "go-test",
	Email: "go-test@example.com",
	When:  time.Now(),
}

// createGitRepo initialises a test repository with two branches:
//   - main:     stack.yaml contains "channel1"
//   - channel2: stack.yaml contains "channel2"
//
// Returns with the repo worktree on channel2 (matching original behaviour).
func createGitRepo(t *testing.T, _ *log.Entry, dir string) {
	t.Helper()

	repo, err := gogit.PlainInitWithOptions(dir, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	// initial commit on main
	setupExampleConfig(t, dir, "channel1")
	err = wt.AddGlob("cluster")
	require.NoError(t, err)
	_, err = wt.Commit("initial commit", &gogit.CommitOptions{Author: testAuthor})
	require.NoError(t, err)

	// create and switch to channel2
	err = wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("channel2"),
		Create: true,
	})
	require.NoError(t, err)

	setupExampleConfig(t, dir, "channel2")
	err = wt.AddGlob("cluster")
	require.NoError(t, err)
	_, err = wt.Commit("branch commit", &gogit.CommitOptions{Author: testAuthor})
	require.NoError(t, err)
}

// addCommit adds a new commit on the given branch in the repo at dir.
func addCommit(t *testing.T, dir, branch, stackContent, message string) {
	t.Helper()

	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	err = wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
	})
	require.NoError(t, err)

	setupExampleConfig(t, dir, stackContent)
	err = wt.AddGlob("cluster")
	require.NoError(t, err)
	_, err = wt.Commit(message, &gogit.CommitOptions{Author: testAuthor})
	require.NoError(t, err)
}

// deleteBranch removes a local branch from the repo at dir.
func deleteBranch(t *testing.T, dir, branch string) {
	t.Helper()

	repo, err := gogit.PlainOpen(dir)
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

func TestGitGarbageCollect(t *testing.T) {
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

	gc, ok := c.(GarbageCollector)
	require.True(t, ok)

	err = gc.GarbageCollect(context.Background(), logger)
	require.NoError(t, err)
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
