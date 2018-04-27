package channel

import (
	"os"
	"os/exec"
	"path"
	"testing"
)

// helper function to setup a test repository.
func createGitRepo(t *testing.T, dir string) {
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	cmd := exec.Command("git", "-C", dir, "init")
	err = cmd.Run()
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	f, err := os.Create(path.Join(dir, "init_file"))
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	err = f.Close()
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	cmd = exec.Command("git", "-C", dir, "add", "init_file")
	err = cmd.Run()
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	cmd = exec.Command("git", "-C", dir, "commit", "-m", "init commit")
	cmd.Env = []string{
		"GIT_AUTHOR_EMAIL=go-test",
		"GIT_AUTHOR_NAME=go-test",
		"GIT_COMMITTER_EMAIL=go-test",
		"GIT_COMMITTER_NAME=go-test",
	}
	err = cmd.Run()
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}
}

func TestGitGet(t *testing.T) {
	workdir := "workdir_test"
	tmpRepo := "tmp_test_repo.git"
	channel := "master"
	createGitRepo(t, tmpRepo)
	defer os.RemoveAll(tmpRepo)

	c, err := NewGit(workdir, tmpRepo, "")
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	err = c.Update()
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	_, err = c.Get(channel)
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	// test geting channel when the repo has already been cloned once. E.i.
	// do a pull in that case.
	_, err = c.Get(channel)
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}

	// cleanup repo
	err = os.RemoveAll(workdir)
	if err != nil {
		t.Errorf("should not fail: %s", err)
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
