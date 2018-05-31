package command

import (
	"bytes"
	"os/exec"
	"sync"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type syncWriter struct {
	sync.Mutex
	buf bytes.Buffer
}

type testLogger struct {
	writer *syncWriter
	entry  *log.Entry
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.Lock()
	n, err := w.buf.Write(p)
	w.Unlock()
	return n, err
}

func (w *syncWriter) String() string {
	w.Lock()
	res := w.buf.String()
	w.Unlock()
	return res
}

func newTestLogger(level log.Level) *testLogger {
	buf := &syncWriter{}

	logger := log.New()
	logger.Out = buf
	logger.Level = level

	return &testLogger{
		writer: buf,
		entry:  log.NewEntry(logger),
	}
}

func TestRunSilentlySuccessful(t *testing.T) {
	cmd := exec.Command("go", "version")
	logger := newTestLogger(log.InfoLevel)
	out, err := RunSilently(logger.entry, cmd)
	require.NoError(t, err)
	require.Contains(t, out, "go version")
	require.Empty(t, logger.writer.String())
}

func TestRunSilentlyDebug(t *testing.T) {
	cmd := exec.Command("go", "version")
	logger := newTestLogger(log.DebugLevel)
	out, err := RunSilently(logger.entry, cmd)
	require.NoError(t, err)
	require.Contains(t, out, "go version")
	require.NotEmpty(t, logger.writer.String())
}

func TestRunSilentlyFailing(t *testing.T) {
	cmd := exec.Command("go", "dsjghsdgkjshgkjsdhfjkshfkjsdf")
	logger := newTestLogger(log.InfoLevel)
	out, err := RunSilently(logger.entry, cmd)
	require.Error(t, err)
	require.Contains(t, out, "unknown subcommand")
	require.NotEmpty(t, logger.writer.String())
}

func TestRunSuccessful(t *testing.T) {
	cmd := exec.Command("go", "dsjghsdgkjshgkjsdhfjkshfkjsdf")
	logger := newTestLogger(log.InfoLevel)
	out, err := Run(logger.entry, cmd)
	require.Error(t, err)
	require.Contains(t, out, "unknown subcommand")
	require.NotEmpty(t, logger.writer.String())
}

func TestRunFailing(t *testing.T) {
	cmd := exec.Command("go", "version")
	logger := newTestLogger(log.InfoLevel)
	out, err := Run(logger.entry, cmd)
	require.NoError(t, err)
	require.Contains(t, out, "go version")
	require.NotEmpty(t, logger.writer.String())
}
