package command

import (
	"os/exec"
	"testing"

	log "github.com/sirupsen/logrus"
)

func TestSuccessfulRun(t *testing.T) {
	cmd := exec.Command("go", "version")
	err := Run(log.StandardLogger(), cmd)
	if err != nil {
		t.Errorf("should not fail: %s", err)
	}
}

func TestFailingRun(t *testing.T) {
	errorMsg := `exec: "invalid-command": executable file not found in $PATH: `

	cmd := exec.Command("invalid-command")

	err := Run(log.StandardLogger(), cmd)
	if err == nil {
		t.Errorf("expected running invalid command to fail")
	}

	if err.Error() != errorMsg {
		t.Errorf("expected error '%s', got '%s'", errorMsg, err.Error())
	}
}
