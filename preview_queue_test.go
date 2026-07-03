package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPreviewQueueRunsOneJobAtATimeAndUsesPriority(t *testing.T) {
	q := newPreviewQueue(4)
	started := make(chan string, 4)
	releaseFirst := make(chan struct{})

	firstDone := make(chan error, 1)
	go func() {
		_, err := q.submit(context.Background(), previewPriorityGuest, func(context.Context) (previewResponse, error) {
			started <- "running-guest"
			<-releaseFirst
			return previewResponse{Planet: "running-guest"}, nil
		})
		firstDone <- err
	}()
	if got := waitStarted(t, started); got != "running-guest" {
		t.Fatalf("first started = %q, want running-guest", got)
	}

	guestDone := submitNamedPreview(t, q, previewPriorityGuest, "queued-guest", started)
	waitQueuedPreviewJobs(t, q, 1)
	userDone := submitNamedPreview(t, q, previewPriorityUser, "queued-user", started)
	waitQueuedPreviewJobs(t, q, 2)
	adminDone := submitNamedPreview(t, q, previewPriorityAdmin, "queued-admin", started)
	waitQueuedPreviewJobs(t, q, 3)

	close(releaseFirst)
	if err := waitErr(t, firstDone); err != nil {
		t.Fatalf("first job err = %v", err)
	}
	for _, want := range []string{"queued-admin", "queued-user", "queued-guest"} {
		if got := waitStarted(t, started); got != want {
			t.Fatalf("next started = %q, want %q", got, want)
		}
	}
	for name, done := range map[string]chan error{"admin": adminDone, "user": userDone, "guest": guestDone} {
		if err := waitErr(t, done); err != nil {
			t.Fatalf("%s job err = %v", name, err)
		}
	}
}

func TestPreviewQueueRejectsWhenFull(t *testing.T) {
	q := newPreviewQueue(1)
	started := make(chan string, 2)
	releaseFirst := make(chan struct{})

	firstDone := make(chan error, 1)
	go func() {
		_, err := q.submit(context.Background(), previewPriorityGuest, func(context.Context) (previewResponse, error) {
			started <- "running"
			<-releaseFirst
			return previewResponse{}, nil
		})
		firstDone <- err
	}()
	if got := waitStarted(t, started); got != "running" {
		t.Fatalf("first started = %q, want running", got)
	}

	queuedDone := submitNamedPreview(t, q, previewPriorityGuest, "queued", started)
	waitQueuedPreviewJobs(t, q, 1)
	_, err := q.submit(context.Background(), previewPriorityAdmin, func(context.Context) (previewResponse, error) {
		return previewResponse{}, nil
	})
	if !errors.Is(err, errPreviewQueueFull) {
		t.Fatalf("full queue err = %v, want errPreviewQueueFull", err)
	}

	close(releaseFirst)
	if err := waitErr(t, firstDone); err != nil {
		t.Fatalf("first job err = %v", err)
	}
	if got := waitStarted(t, started); got != "queued" {
		t.Fatalf("queued started = %q, want queued", got)
	}
	if err := waitErr(t, queuedDone); err != nil {
		t.Fatalf("queued job err = %v", err)
	}
}

func submitNamedPreview(t *testing.T, q *previewQueue, priority int, name string, started chan<- string) chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := q.submit(context.Background(), priority, func(context.Context) (previewResponse, error) {
			started <- name
			return previewResponse{Planet: name}, nil
		})
		done <- err
	}()
	return done
}

func waitQueuedPreviewJobs(t *testing.T, q *previewQueue, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		q.mu.Lock()
		got := len(q.jobs)
		q.mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	q.mu.Lock()
	got := len(q.jobs)
	q.mu.Unlock()
	t.Fatalf("queued preview jobs = %d, want at least %d", got, want)
}

func waitStarted(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case got := <-started:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for preview job to start")
		return ""
	}
}

func waitErr(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for preview job to finish")
		return nil
	}
}
