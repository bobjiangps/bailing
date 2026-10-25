package imaging

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// Queue runs image jobs on a fixed number of workers.
// High-priority jobs (a user is waiting) jump ahead of background jobs.
// Jobs with the same key are de-duplicated: a second submit returns the
// already queued / running job.
type Queue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	hi, lo   []*Job
	inflight map[string]*Job
	closed   bool
	pending  atomic.Int64
	wg       sync.WaitGroup
}

// Job is a unit of work; Wait blocks until it finished.
type Job struct {
	key  string
	fn   func() error
	done chan struct{}
	err  error
}

// NewQueue starts n workers.
func NewQueue(n int) *Queue {
	if n < 1 {
		n = 1
	}
	q := &Queue{inflight: map[string]*Job{}}
	q.cond = sync.NewCond(&q.mu)
	for i := 0; i < n; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

// Submit enqueues fn under key. If the key is already pending it returns the
// existing job (and promotes it to high priority when requested).
func (q *Queue) Submit(key string, fn func() error, high bool) *Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	if j, ok := q.inflight[key]; ok {
		if high {
			q.promote(j)
		}
		return j
	}
	j := &Job{key: key, fn: fn, done: make(chan struct{})}
	if q.closed {
		j.err = errors.New("queue closed")
		close(j.done)
		return j
	}
	q.inflight[key] = j
	if high {
		q.hi = append(q.hi, j)
	} else {
		q.lo = append(q.lo, j)
	}
	q.pending.Add(1)
	q.cond.Signal()
	return j
}

// promote moves a queued low-priority job to the high queue (caller holds mu).
func (q *Queue) promote(j *Job) {
	for i, x := range q.lo {
		if x == j {
			q.lo = append(q.lo[:i], q.lo[i+1:]...)
			q.hi = append(q.hi, j)
			return
		}
	}
}

// Pending returns the number of queued + running jobs.
func (q *Queue) Pending() int64 { return q.pending.Load() }

// Close stops accepting jobs and waits for workers to drain the high queue.
func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	for _, j := range q.lo { // background jobs are dropped; release their waiters
		j.err = errors.New("queue closed")
		delete(q.inflight, j.key)
		q.pending.Add(-1)
		close(j.done)
	}
	q.lo = nil
	q.cond.Broadcast()
	q.mu.Unlock()
	q.wg.Wait()
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		q.mu.Lock()
		for len(q.hi) == 0 && len(q.lo) == 0 && !q.closed {
			q.cond.Wait()
		}
		var j *Job
		switch {
		case len(q.hi) > 0:
			j, q.hi = q.hi[0], q.hi[1:]
		case len(q.lo) > 0:
			j, q.lo = q.lo[0], q.lo[1:]
		default: // closed and empty
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()

		j.err = safeRun(j.fn)

		q.mu.Lock()
		delete(q.inflight, j.key)
		q.mu.Unlock()
		q.pending.Add(-1)
		close(j.done)
	}
}

func safeRun(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("image job panicked")
		}
	}()
	return fn()
}

// Wait blocks until the job is done or ctx is cancelled.
func (j *Job) Wait(ctx context.Context) error {
	select {
	case <-j.done:
		return j.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
