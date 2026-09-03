package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

func TestRollbackTransactionOutlivesCanceledRequest(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	transaction := &coreRollbackContextRecorder{}
	if err := rollbackTransaction(requestContext, transaction); err != nil {
		t.Fatal(err)
	}
	if transaction.canceled {
		t.Fatal("rollback inherited the canceled request context")
	}
}

func TestCommittedUploadCleanupTimeoutAndAdmissionBoundHungBackends(t *testing.T) {
	local, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &hungCleanupBackend{
		Backend: local,
		entered: make(chan string, 2),
		release: make(chan struct{}),
		done:    make(chan struct{}, 2),
	}
	released := false
	defer func() {
		if !released {
			close(backend.release)
		}
	}()
	application, err := New(Config{
		Name: "bounded committed cleanup", Storage: backend, StorageNamespace: "bounded-committed-cleanup",
		Collections: []Collection{{Slug: "media", Upload: true, UploadConfig: UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Upload(context.Background(), "media", UploadInput{Filename: "first.txt", Reader: strings.NewReader("first")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Upload(context.Background(), "media", UploadInput{Filename: "second.txt", Reader: strings.NewReader("second")})
	if err != nil {
		t.Fatal(err)
	}
	application.uploadCleanupTimeout = 25 * time.Millisecond
	application.uploadCleanupAdmission = make(chan struct{}, 1)

	firstDone := make(chan error, 1)
	go func() {
		_, deleteError := application.Local().Delete(context.Background(), "media", first.ID, nil)
		firstDone <- deleteError
	}()
	select {
	case <-backend.entered:
	case <-time.After(time.Second):
		t.Fatal("first cleanup did not reach the blocking backend")
	}
	start := time.Now()
	_, secondError := application.Local().Delete(context.Background(), "media", second.ID, nil)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("admission timeout took %s", elapsed)
	}
	assertCommittedCleanupDeadline(t, secondError)
	select {
	case key := <-backend.entered:
		t.Fatalf("second cleanup bypassed bounded admission and reached Delete(%q)", key)
	default:
	}
	select {
	case firstError := <-firstDone:
		assertCommittedCleanupDeadline(t, firstError)
	case <-time.After(time.Second):
		t.Fatal("hung backend kept the first post-commit request blocked")
	}
	secondKey, _ := second.Values["objectKey"].StringValue()
	if reader, _, openError := local.Open(context.Background(), secondKey); openError != nil {
		t.Fatalf("timed-out cleanup removed the retained reconciliation candidate: %v", openError)
	} else {
		_ = reader.Close()
	}
	close(backend.release)
	released = true
	select {
	case <-backend.done:
	case <-time.After(time.Second):
		t.Fatal("blocking cleanup worker did not exit after backend release")
	}
}

func assertCommittedCleanupDeadline(t *testing.T, err error) {
	t.Helper()
	var operationError *OperationError
	if !errors.As(err, &operationError) || operationError.Code != "hook_failed" || !operationError.Committed || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanup deadline error = %#v, %v", operationError, err)
	}
}

type hungCleanupBackend struct {
	storage.Backend
	entered chan string
	release chan struct{}
	done    chan struct{}
}

func (backend *hungCleanupBackend) Delete(ctx context.Context, key string) error {
	backend.entered <- key
	<-backend.release
	err := backend.Backend.Delete(ctx, key)
	backend.done <- struct{}{}
	return err
}

type coreRollbackContextRecorder struct {
	store.Transaction
	canceled bool
}

func (transaction *coreRollbackContextRecorder) Rollback(ctx context.Context) error {
	transaction.canceled = ctx.Err() != nil
	return nil
}
