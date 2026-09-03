package uploads

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/semaphore"
)

const (
	maximumUploadBytes      int64 = 256 << 20
	defaultFileBudgetBytes        = 512 << 20
	defaultImageBudgetBytes       = 512 << 20
	defaultQueuedUploads          = 64
)

// WorkAdmission bounds compressed upload bytes and decoded image work across
// every application hosted by one Ridu process.
type WorkAdmission struct {
	files          *semaphore.Weighted
	images         *semaphore.Weighted
	fileBudget     int64
	imageBudget    int64
	queue          chan struct{}
	queuedCapacity int
}

var processWorkAdmission = NewWorkAdmission(defaultFileBudgetBytes, defaultImageBudgetBytes, defaultQueuedUploads)

// NewWorkAdmission constructs an isolated admission controller. It is public
// within the internal package so resource-bound behavior can be proven without
// consuming the process-wide production budget in tests.
func NewWorkAdmission(fileBudgetBytes, imageBudgetBytes int64, queued int) *WorkAdmission {
	if fileBudgetBytes < 1 {
		fileBudgetBytes = 1
	}
	if imageBudgetBytes < 1 {
		imageBudgetBytes = 1
	}
	if queued < 1 {
		queued = 1
	}
	return &WorkAdmission{
		files: semaphore.NewWeighted(fileBudgetBytes), images: semaphore.NewWeighted(imageBudgetBytes),
		fileBudget: fileBudgetBytes, imageBudget: imageBudgetBytes,
		queue: make(chan struct{}, queued), queuedCapacity: queued,
	}
}

// BusyError means the bounded admission queue is full. Callers should retry
// after backoff instead of treating it as invalid content.
type BusyError struct{ Resource string }

func (err *BusyError) Error() string {
	return fmt.Sprintf("%s processing capacity is busy; try again shortly", err.Resource)
}

func IsBusy(err error) bool {
	var busy *BusyError
	return errors.As(err, &busy)
}

func (admission *WorkAdmission) AcquireFile(ctx context.Context, bytes int64) (func(), error) {
	if bytes < 1 || bytes > admission.fileBudget {
		return nil, fmt.Errorf("upload requires %d bytes, exceeding the %d-byte process budget", bytes, admission.fileBudget)
	}
	select {
	case admission.queue <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, &BusyError{Resource: "upload"}
	}
	if err := admission.files.Acquire(ctx, bytes); err != nil {
		<-admission.queue
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			admission.files.Release(bytes)
			<-admission.queue
		})
	}, nil
}

func (admission *WorkAdmission) AcquireImage(ctx context.Context, bytes int64) (func(), error) {
	if bytes < 1 || bytes > admission.imageBudget {
		return nil, fmt.Errorf("image requires %d bytes, exceeding the %d-byte process budget", bytes, admission.imageBudget)
	}
	if err := admission.images.Acquire(ctx, bytes); err != nil {
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { admission.images.Release(bytes) }) }, nil
}

func (manager Manager) workAdmission() *WorkAdmission {
	if manager.Admission != nil {
		return manager.Admission
	}
	return processWorkAdmission
}

// AcquireFile reserves compressed-byte capacity for a caller, such as remote
// download handling, that must allocate before Prepare receives its reader.
func (manager Manager) AcquireFile(ctx context.Context, bytes int64) (func(), error) {
	return manager.workAdmission().AcquireFile(ctx, bytes)
}
