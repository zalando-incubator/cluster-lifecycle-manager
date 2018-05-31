package command

import (
	"os/exec"
	"strings"

	"bytes"
	log "github.com/sirupsen/logrus"
	"io"
)

func outputLines(output string) []string {
	return strings.Split(strings.TrimRight(output, "\n"), "\n")
}

// RunSilently runs an exec.Cmd, capturing its output and additionally logging it
// only if the command fails (or if debug logging is enabled)
func RunSilently(logger *log.Entry, cmd *exec.Cmd) (string, error) {
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
func Run(logger *log.Entry, cmd *exec.Cmd) (string, error) {
	var output bytes.Buffer

	cmd.Stdout = io.MultiWriter(&output, logger.WriterLevel(log.InfoLevel))
	cmd.Stderr = io.MultiWriter(&output, logger.WriterLevel(log.ErrorLevel))

	err := cmd.Run()
	return output.String(), err
}
