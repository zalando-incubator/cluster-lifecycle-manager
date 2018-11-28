package command

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
)

// ExecManager limits the number of external commands that are running at the same time
type ExecManager struct {
	sema *semaphore.Weighted
}

func NewExecManager(maxConcurrency uint) *ExecManager {
	return &ExecManager{
		sema: semaphore.NewWeighted(int64(maxConcurrency)),
	}
}

func outputLines(output string) []string {
	return strings.Split(strings.TrimRight(output, "\n"), "\n")
}

// RunSilently runs an exec.Cmd, capturing its output and additionally logging it
// only if the command fails (or if debug logging is enabled)
func (m *ExecManager) RunSilently(ctx context.Context, logger *log.Entry, cmd *exec.Cmd) (string, error) {
	err := m.sema.Acquire(ctx, 1)
	if err != nil {
		return "", err
	}
	defer m.sema.Release(1)

	rawOut, err := cmd.CombinedOutput()
	out := string(rawOut)
	if err != nil {
		for _, line := range outputLines(out) {
			logger.Errorln(line)
		}
	} else if logger.Logger.Level >= log.DebugLevel {
		for _, line := range outputLines(out) {
			logger.Debugln(line)
		}
	}
	return string(out), err
}

// Run runs an exec.Cmd, capturing its output and additionally redirecting
// it to a logger
func (m *ExecManager) Run(ctx context.Context, logger *log.Entry, cmd *exec.Cmd) (string, error) {
	err := m.sema.Acquire(ctx, 1)
	if err != nil {
		return "", err
	}
	defer m.sema.Release(1)

	var output bytes.Buffer

	cmd.Stdout = io.MultiWriter(&output, logger.WriterLevel(log.InfoLevel))
	cmd.Stderr = io.MultiWriter(&output, logger.WriterLevel(log.ErrorLevel))

	err = cmd.Run()
	return output.String(), err
}
