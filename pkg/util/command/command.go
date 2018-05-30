package command

import (
	"os/exec"

	log "github.com/sirupsen/logrus"
	"strings"
)

func outputLines(raw []byte) []string {
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

// Run configures stdout and stderr of a command to use the logger interface
// and adds stderr as context if the command exits with status code > 0.
func Run(logger *log.Entry, cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	if err != nil {
		for _, line := range outputLines(out) {
			logger.Errorln(line)
		}
	} else if logger.Logger.Level >= log.DebugLevel {
		for _, line := range outputLines(out) {
			logger.Debugln(line)
		}
	}
	return err
}
