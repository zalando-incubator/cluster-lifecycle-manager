package command

import (
	"os/exec"
	"testing"

	"bytes"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type testLogger struct {
	buf   *bytes.Buffer
	entry *log.Entry
}

func newTestLogger(level log.Level) *testLogger {
	buf := &bytes.Buffer{}

	logger := log.New()
	logger.Out = buf
	logger.Level = level

	return &testLogger{
		buf:   buf,
		entry: log.NewEntry(logger),
	}
}

func TestSuccessfulRun(t *testing.T) {
	cmd := exec.Command("go", "version")
	logger := newTestLogger(log.InfoLevel)
	err := Run(logger.entry, cmd)
	require.NoError(t, err)
	require.Empty(t, logger.buf.String())
}

func TestSuccessfulRunDebug(t *testing.T) {
	cmd := exec.Command("go", "version")
	logger := newTestLogger(log.DebugLevel)
	err := Run(logger.entry, cmd)
	require.NoError(t, err)
	require.NotEmpty(t, logger.buf.String())
}

func TestFailingRun(t *testing.T) {
	cmd := exec.Command("go", "dsjghsdgkjshgkjsdhfjkshfkjsdf")
	logger := newTestLogger(log.InfoLevel)
	err := Run(logger.entry, cmd)
	require.Error(t, err)
	require.NotEmpty(t, logger.buf.String())
}
