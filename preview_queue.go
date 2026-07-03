package main

import (
	"context"
	"errors"
	"sync"
)

const (
	previewPriorityGuest = iota + 1
	previewPriorityUser
	previewPriorityAdmin
)

type previewJobResult struct {
	response previewResponse
	err      error
}

type previewJob struct {
	priority int
	seq      int64
	ctx      context.Context
	run      func(context.Context) (previewResponse, error)
	result   chan previewJobResult
}

type previewQueue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	max     int
	nextSeq int64
	jobs    []*previewJob
}

var errPreviewQueueFull = errors.New("preview generation queue is full; try again shortly")

func newPreviewQueue(max int) *previewQueue {
	if max < 1 {
		max = 1
	}
	q := &previewQueue{max: max}
	q.cond = sync.NewCond(&q.mu)
	go q.worker()
	return q
}

func (q *previewQueue) submit(ctx context.Context, priority int, run func(context.Context) (previewResponse, error)) (previewResponse, error) {
	job := &previewJob{
		priority: priority,
		ctx:      ctx,
		run:      run,
		result:   make(chan previewJobResult, 1),
	}

	q.mu.Lock()
	if len(q.jobs) >= q.max {
		q.mu.Unlock()
		return previewResponse{}, errPreviewQueueFull
	}
	q.nextSeq++
	job.seq = q.nextSeq
	q.jobs = append(q.jobs, job)
	q.cond.Signal()
	q.mu.Unlock()

	select {
	case result := <-job.result:
		return result.response, result.err
	case <-ctx.Done():
		if q.remove(job) {
			return previewResponse{}, ctx.Err()
		}
		result := <-job.result
		return result.response, result.err
	}
}

func (q *previewQueue) remove(job *previewJob) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, queued := range q.jobs {
		if queued == job {
			q.jobs = append(q.jobs[:i], q.jobs[i+1:]...)
			return true
		}
	}
	return false
}

func (q *previewQueue) worker() {
	for {
		job := q.next()
		if err := job.ctx.Err(); err != nil {
			job.result <- previewJobResult{err: err}
			continue
		}
		response, err := job.run(job.ctx)
		job.result <- previewJobResult{response: response, err: err}
	}
}

func (q *previewQueue) next() *previewJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.jobs) == 0 {
		q.cond.Wait()
	}

	best := 0
	for i := 1; i < len(q.jobs); i++ {
		if q.jobs[i].priority > q.jobs[best].priority || (q.jobs[i].priority == q.jobs[best].priority && q.jobs[i].seq < q.jobs[best].seq) {
			best = i
		}
	}
	job := q.jobs[best]
	q.jobs = append(q.jobs[:best], q.jobs[best+1:]...)
	return job
}
