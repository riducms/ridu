package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeUploadLockSession struct {
	failures   map[int]error
	statements []string
	keys       []string
	releases   int
	discards   int
}

func (session *fakeUploadLockSession) Exec(_ context.Context, statement, key string) error {
	call := len(session.statements)
	session.statements = append(session.statements, statement)
	session.keys = append(session.keys, key)
	return session.failures[call]
}

func (session *fakeUploadLockSession) Release() { session.releases++ }
func (session *fakeUploadLockSession) Discard() { session.discards++ }

func TestAcquirePostgresUploadLocksDiscardsSessionAfterAnyAcquisitionError(t *testing.T) {
	failure := errors.New("lock round trip failed")
	for _, failingCall := range []int{0, 1} {
		t.Run(fmt.Sprintf("lock_%d", failingCall+1), func(t *testing.T) {
			session := &fakeUploadLockSession{failures: map[int]error{failingCall: failure}}
			release, err := acquirePostgresUploadLocks(context.Background(), session, []string{"object-a", "object-b"})
			if !errors.Is(err, failure) || release != nil {
				t.Fatalf("acquisition result = release %v, error %v", release != nil, err)
			}
			if session.discards != 1 || session.releases != 0 {
				t.Fatalf("failed acquisition session disposition = discards %d, releases %d", session.discards, session.releases)
			}
			if len(session.statements) != failingCall+1 {
				t.Fatalf("acquisition calls = %d, want %d", len(session.statements), failingCall+1)
			}
		})
	}
}

func TestAcquirePostgresUploadLocksReleasesSessionOnlyAfterCleanReverseUnlock(t *testing.T) {
	session := &fakeUploadLockSession{failures: map[int]error{}}
	release, err := acquirePostgresUploadLocks(context.Background(), session, []string{"object-a", "object-b"})
	if err != nil {
		t.Fatal(err)
	}
	if session.releases != 0 || session.discards != 0 {
		t.Fatalf("acquired session was finalized early: releases %d, discards %d", session.releases, session.discards)
	}
	release()
	release()
	if session.releases != 1 || session.discards != 0 {
		t.Fatalf("clean session disposition = releases %d, discards %d", session.releases, session.discards)
	}
	if len(session.statements) != 4 {
		t.Fatalf("lock/unlock calls = %d", len(session.statements))
	}
	for index := 2; index < 4; index++ {
		if !strings.Contains(session.statements[index], "pg_advisory_unlock") {
			t.Fatalf("release statement %d = %q", index, session.statements[index])
		}
	}
	if session.keys[2] != "object-b" || session.keys[3] != "object-a" {
		t.Fatalf("unlock order = %v", session.keys[2:])
	}
}

func TestReleasePostgresUploadLocksDiscardsSessionAfterAnyUnlockError(t *testing.T) {
	failure := errors.New("unlock round trip failed")
	for _, failingCall := range []int{2, 3} {
		t.Run(fmt.Sprintf("unlock_%d", failingCall-1), func(t *testing.T) {
			session := &fakeUploadLockSession{failures: map[int]error{failingCall: failure}}
			release, err := acquirePostgresUploadLocks(context.Background(), session, []string{"object-a", "object-b"})
			if err != nil {
				t.Fatal(err)
			}
			release()
			release()
			if session.discards != 1 || session.releases != 0 {
				t.Fatalf("failed unlock session disposition = discards %d, releases %d", session.discards, session.releases)
			}
			if len(session.statements) != failingCall+1 {
				t.Fatalf("lock/unlock calls = %d, want %d", len(session.statements), failingCall+1)
			}
		})
	}
}
