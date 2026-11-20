package updatestrategy

import (
	"context"
	"errors"
	"sync"
)

// ErrUpdateRestarted is returned when an update stops because a restart was
// requested, to be started again with a newer version. Nodes already marked
// for decommission are left as they are, so the next run continues with them,
// as it does after a restart of the process.
var ErrUpdateRestarted = errors.New("update restarted")

// RestartSignal asks an update in progress to stop at the next safe point.
// Unlike cancelling the context it doesn't interrupt anything by itself: each
// step decides locally whether it can be stopped, by calling CheckRestart or
// waiting on RestartRequested.
type RestartSignal struct {
	once sync.Once
	ch   chan struct{}
}

func NewRestartSignal() *RestartSignal {
	return &RestartSignal{ch: make(chan struct{})}
}

// Request marks the restart as requested. It's safe to call multiple times
// and on a nil signal.
func (s *RestartSignal) Request() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.ch) })
}

type restartSignalKey struct{}

// WithRestartSignal returns a context carrying the signal.
func WithRestartSignal(ctx context.Context, s *RestartSignal) context.Context {
	return context.WithValue(ctx, restartSignalKey{}, s)
}

// RestartRequested returns a channel that is closed once a restart is
// requested. It never fires if the context carries no signal.
func RestartRequested(ctx context.Context) <-chan struct{} {
	if s, ok := ctx.Value(restartSignalKey{}).(*RestartSignal); ok && s != nil {
		return s.ch
	}
	return nil
}

// CheckRestart returns ErrUpdateRestarted if a restart was requested.
func CheckRestart(ctx context.Context) error {
	select {
	case <-RestartRequested(ctx):
		return ErrUpdateRestarted
	default:
		return nil
	}
}
