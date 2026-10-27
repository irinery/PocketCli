package web

import (
	"context"
	"errors"
	"sync"
)

// ReportJobs keeps PDF exports alive after a browser disconnects and lets the
// owning server stop and wait for them during shutdown.
type ReportJobs struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	active sync.WaitGroup
}

func NewReportJobs() *ReportJobs {
	ctx, cancel := context.WithCancel(context.Background())
	return &ReportJobs{ctx: ctx, cancel: cancel}
}

func (j *ReportJobs) Begin() (context.Context, func(), error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil, nil, errors.New("ERR_PDF_FAILED: servidor local encerrando")
	}
	j.active.Add(1)
	return j.ctx, j.active.Done, nil
}

func (j *ReportJobs) Close(ctx context.Context) error {
	j.mu.Lock()
	j.closed = true
	j.cancel()
	j.mu.Unlock()
	done := make(chan struct{})
	go func() {
		j.active.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
