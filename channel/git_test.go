package channel

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
)

// helper function to setup a test repository.
func createGitRepo(t *testing.T, dir string) {
	err := os.MkdirAll(dir, 0755)
	require.NoError(t, err)

	err = exec.Command("git", "-C", dir, "init").Run()
	require.NoError(t, err)

	commit := func(message string) {
		cmd := exec.Command("git", "-C", dir, "commit", "-am", message)
		cmd.Env = []string{
			"GIT_AUTHOR_EMAIL=go-test",
			"GIT_AUTHOR_NAME=go-test",
			"GIT_COMMITTER_EMAIL=go-test",
			"GIT_COMMITTER_NAME=go-test",
		}
		log.Printf("test")
		err := command.Run(log.StandardLogger(), cmd)
		require.NoError(t, err)
	}

	err = ioutil.WriteFile(path.Join(dir, "init_file"), []byte{}, 0644)
	require.NoError(t, err)

	err = exec.Command("git", "-C", dir, "add", "init_file").Run()
	require.NoError(t, err)

	commit("initial commit")

	err = exec.Command("git", "-C", dir, "checkout", "-b", "channel2").Run()
	require.NoError(t, err)

	err = ioutil.WriteFile(path.Join(dir, "different_file"), []byte{}, 0644)
	require.NoError(t, err)

	err = exec.Command("git", "-C", dir, "add", "different_file").Run()
	require.NoError(t, err)

	commit("branch commit")
}

func checkout(t *testing.T, source ConfigSource, versions ConfigVersions, channel string) string {
	version, err := versions.Version(channel)
	require.NoError(t, err)

	checkout, err := source.Get(version)
	require.NoError(t, err)

	return checkout.Path
}

func requireFile(t *testing.T, dir string, file string) {
	_, err := os.Stat(path.Join(dir, file))
	require.NoError(t, err)
}

func requireNoFile(t *testing.T, dir string, file string) {
	_, err := os.Stat(path.Join(dir, file))
	require.NotNil(t, err)
	require.True(t, os.IsNotExist(err), "unexpected error: %v", err)
}

func TestGitGet(t *testing.T) {
	workdir := "workdir_test"
	tmpRepo := "tmp_test_repo.git"
	createGitRepo(t, tmpRepo)
	defer os.RemoveAll(tmpRepo)

	c, err := NewGit(workdir, tmpRepo, "")
	require.NoError(t, err)
	defer os.RemoveAll(workdir)

	versions, err := c.Update()
	require.NoError(t, err)

	// check master channel
	master := checkout(t, c, versions, "master")
	requireFile(t, master, "init_file")
	requireNoFile(t, master, "different_file")

	// check another channel
	channel2 := checkout(t, c, versions, "channel2")
	requireFile(t, channel2, "init_file")
	requireFile(t, channel2, "different_file")

	// check sha
	out, err := exec.Command("git", "-C", tmpRepo, "rev-parse", "master").Output()
	require.NoError(t, err)

	sha := checkout(t, c, versions, strings.TrimSpace(string(out)))
	requireFile(t, sha, "init_file")
	requireNoFile(t, sha, "different_file")
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
