package channel

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
)

// helper function to setup a test repository.
func createGitRepo(t *testing.T, logger *log.Entry, dir string) {
	err := exec.Command("git", "-C", dir, "init").Run()
	require.NoError(t, err)

	execManager := command.NewExecManager(1)

	commit := func(message string) {
		cmd := exec.Command("git", "-C", dir, "commit", "-am", message)
		cmd.Env = []string{
			"GIT_AUTHOR_EMAIL=go-test",
			"GIT_AUTHOR_NAME=go-test",
			"GIT_COMMITTER_EMAIL=go-test",
			"GIT_COMMITTER_NAME=go-test",
		}
		log.Printf("test")
		_, err := execManager.RunSilently(context.Background(), logger, cmd)
		require.NoError(t, err)
	}

	setupExampleConfig(t, dir, "channel1")

	err = exec.Command("git", "-C", dir, "add", "cluster").Run()
	require.NoError(t, err)

	commit("initial commit")

	err = exec.Command("git", "-C", dir, "checkout", "-b", "channel2").Run()
	require.NoError(t, err)

	setupExampleConfig(t, dir, "channel2")

	err = exec.Command("git", "-C", dir, "add", "cluster").Run()
	require.NoError(t, err)

	commit("branch commit")
}

func checkout(t *testing.T, logger *log.Entry, source ConfigSource, channel string) Config {
	version, err := source.Version(channel, nil)
	require.NoError(t, err)

	checkout, err := version.Get(context.Background(), logger)
	require.NoError(t, err)

	return checkout
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

	// check master channel
	master := checkout(t, logger, c, "master")
	verifyExampleConfig(t, master, "testsrc", "channel1")

	// check another channel
	channel2 := checkout(t, logger, c, "channel2")
	verifyExampleConfig(t, channel2, "testsrc", "channel2")

	// check sha
	out, err := exec.Command(
		"git",
		"-C",
		repoTempDir,
		"rev-parse",
		"master",
	).Output()
	require.NoError(t, err)

	sha := checkout(t, logger, c, strings.TrimSpace(string(out)))
	verifyExampleConfig(t, sha, "testsrc", "channel1")
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
