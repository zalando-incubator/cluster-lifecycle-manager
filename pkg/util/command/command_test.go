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

func TestRunSilentlySuccessful(t *testing.T) {
	cmd := exec.Command("go", "version")
	logger := newTestLogger(log.InfoLevel)
	out, err := RunSilently(logger.entry, cmd)
	require.NoError(t, err)
	require.Contains(t, out, "go version")
	require.Empty(t, logger.buf.String())
}

func TestRunSilentlyDebug(t *testing.T) {
	cmd := exec.Command("go", "version")
	logger := newTestLogger(log.DebugLevel)
	out, err := RunSilently(logger.entry, cmd)
	require.NoError(t, err)
	require.Contains(t, out, "go version")
	require.NotEmpty(t, logger.buf.String())
}

func TestRunSilentlyFailing(t *testing.T) {
	cmd := exec.Command("go", "dsjghsdgkjshgkjsdhfjkshfkjsdf")
	logger := newTestLogger(log.InfoLevel)
	out, err := RunSilently(logger.entry, cmd)
	require.Error(t, err)
	require.Contains(t, out, "unknown subcommand")
	require.NotEmpty(t, logger.buf.String())
}

func TestRunSuccessful(t *testing.T) {
	cmd := exec.Command("go", "dsjghsdgkjshgkjsdhfjkshfkjsdf")
	logger := newTestLogger(log.InfoLevel)
	out, err := Run(logger.entry, cmd)
	require.Error(t, err)
	require.Contains(t, out, "unknown subcommand")
	require.NotEmpty(t, logger.buf.String())
}

func TestRunFailing(t *testing.T) {
	cmd := exec.Command("go", "version")
	logger := newTestLogger(log.InfoLevel)
	out, err := Run(logger.entry, cmd)
	require.NoError(t, err)
	require.Contains(t, out, "go version")
	require.NotEmpty(t, logger.buf.String())
}
