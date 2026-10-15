// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/otfabric/modbus-trace-explorer/internal/service"
)

// A job is a scan or an analysis running in its own goroutine.
type job struct {
	e      *Engine
	cancel context.CancelFunc
	done   chan struct{}

	mu       sync.Mutex
	state    service.JobState
	started  time.Time
	lastEmit time.Time
}

// progressInterval rate-limits progress events (at most 10 a second).
const progressInterval = 100 * time.Millisecond

// start runs fn as a job of kind on file and returns its reference.
func (e *Engine) start(kind, file string, fn func(ctx context.Context, j *job) (warning string, err error)) service.JobRef {
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.nextID++
	id := kind + "-" + strconv.Itoa(e.nextID)
	j := &job{e: e, cancel: cancel, done: make(chan struct{}), started: e.now(),
		state: service.JobState{ID: id, Kind: kind, File: file, State: service.JobRunning, Total: -1}}
	e.jobs[id] = j
	e.mu.Unlock()
	j.emit(true)
	go func() {
		defer close(j.done)
		defer cancel()
		warning, err := fn(ctx, j)
		j.mu.Lock()
		switch {
		case err == nil:
			j.state.State = service.JobDone
			j.state.Warning = warning
		case errors.Is(err, context.Canceled):
			j.state.State = service.JobCanceled
		default:
			j.state.State = service.JobFailed
			var se *service.Error
			if !errors.As(err, &se) {
				se = &service.Error{Code: service.CodeInternal, Message: err.Error()}
			}
			j.state.Error = se
		}
		j.mu.Unlock()
		e.log.Info("job finished", "job", id, "state", j.state.State)
		j.emit(true)
		e.mu.Lock()
		delete(e.jobs, id)
		e.mu.Unlock()
	}()
	return service.JobRef{ID: id, Kind: kind}
}

// progress updates the job's counters and emits at most every
// progressInterval.
func (j *job) progress(phase string, read, total int64, rows uint64) {
	j.mu.Lock()
	j.state.Phase, j.state.Read, j.state.Total, j.state.Rows = phase, read, total, rows
	if el := j.e.now().Sub(j.started).Seconds(); el > 0 {
		j.state.Rate = int64(float64(read) / el)
	}
	due := j.e.now().Sub(j.lastEmit) >= progressInterval
	j.mu.Unlock()
	if due {
		j.emit(false)
	}
}

// emit sends the job's state to every subscriber. A final state is always
// delivered (it waits for a slow subscriber); progress is dropped for a
// subscriber that is behind.
func (j *job) emit(final bool) {
	j.mu.Lock()
	st := j.state
	j.lastEmit = j.e.now()
	j.mu.Unlock()
	ev := service.Event{Kind: "job", Job: &st}
	j.e.mu.Lock()
	subs := make([]chan service.Event, 0, len(j.e.subs))
	for ch := range j.e.subs {
		subs = append(subs, ch)
	}
	j.e.mu.Unlock()
	for _, ch := range subs {
		if final {
			select {
			case ch <- ev:
			case <-time.After(5 * time.Second):
			}
			continue
		}
		select {
		case ch <- ev:
		default:
		}
	}
}

// Subscribe delivers job events until ctx ends.
func (e *Engine) Subscribe(ctx context.Context) (<-chan service.Event, error) {
	ch := make(chan service.Event, 64)
	e.mu.Lock()
	e.subs[ch] = struct{}{}
	e.mu.Unlock()
	out := make(chan service.Event)
	go func() {
		defer close(out)
		defer func() {
			e.mu.Lock()
			delete(e.subs, ch)
			e.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-ch:
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// CancelJob cancels a running job; the job reports "canceled" when it has
// stopped.
func (e *Engine) CancelJob(_ context.Context, id string) error {
	e.mu.Lock()
	j := e.jobs[id]
	e.mu.Unlock()
	if j == nil {
		return service.Errorf(service.CodeNotFound, "no running job %s", id)
	}
	j.cancel()
	return nil
}

// wait blocks until a job has finished (for tests and shutdown).
func (e *Engine) wait(id string) {
	e.mu.Lock()
	j := e.jobs[id]
	e.mu.Unlock()
	if j != nil {
		<-j.done
	}
}
