package command

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"

	log "github.com/sirupsen/logrus"
)

// writerLeveler is required to abstract the types passed to the Run function
type writerLeveler interface {
	WriterLevel(level log.Level) *io.PipeWriter
}

// Run configures stdout and stderr of a command to use the logger interface
// and adds stderr as context if the command exits with status code > 0.
func Run(logger writerLeveler, cmd *exec.Cmd) error {
	var stderr bytes.Buffer
	mWriter := io.MultiWriter(&stderr, logger.WriterLevel(log.ErrorLevel))

	cmd.Stdout = logger.WriterLevel(log.DebugLevel)
	cmd.Stderr = mWriter

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%s: %s", err, stderr.String())
	}
	return nil
}
