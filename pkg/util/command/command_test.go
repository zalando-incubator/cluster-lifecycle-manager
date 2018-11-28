package command

import (
	"bytes"
	"context"
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

var (
	testManager = NewExecManager(1)
)

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
	cmd := exec.Command("echo", "foo")
	logger := newTestLogger(log.InfoLevel)
	out, err := testManager.RunSilently(context.Background(), logger.entry, cmd)
	require.NoError(t, err)
	require.Contains(t, out, "foo")
	require.Empty(t, logger.writer.String())
}

func TestRunSilentlyDebug(t *testing.T) {
	cmd := exec.Command("echo", "foo")
	logger := newTestLogger(log.DebugLevel)
	out, err := testManager.RunSilently(context.Background(), logger.entry, cmd)
	require.NoError(t, err)
	require.Contains(t, out, "foo")
	require.NotEmpty(t, logger.writer.String())
}

func TestRunSilentlyFailing(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo foo && false")
	logger := newTestLogger(log.InfoLevel)
	out, err := testManager.RunSilently(context.Background(), logger.entry, cmd)
	require.Error(t, err)
	require.Contains(t, out, "foo")
	require.NotEmpty(t, logger.writer.String())
}

func TestRunSuccessful(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo foo && false")
	logger := newTestLogger(log.InfoLevel)
	out, err := testManager.Run(context.Background(), logger.entry, cmd)
	require.Error(t, err)
	require.Contains(t, out, "foo")
	require.NotEmpty(t, logger.writer.String())
}

func TestRunFailing(t *testing.T) {
	cmd := exec.Command("echo", "foo")
	logger := newTestLogger(log.InfoLevel)
	out, err := testManager.Run(context.Background(), logger.entry, cmd)
	require.NoError(t, err)
	require.Contains(t, out, "foo")
	require.NotEmpty(t, logger.writer.String())
}
