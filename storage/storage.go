// Package storage defines the public object-storage boundary used by upload
// collections. Document stores never receive file bytes.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrNotFound = errors.New("object not found")

// MaxListPageSize bounds one adapter listing response. Reconciliation streams
// pages so a large bucket never has to fit in memory.
const MaxListPageSize = 1_000

type Object struct {
	Key         string
	Size        int64
	ContentType string
	// ModifiedAt must be non-zero for every object returned by List. Ridu uses
	// it to enforce the reconciliation grace period and refuses cleanup when it
	// is unavailable.
	ModifiedAt time.Time
}

// ListRequest selects one bounded, stable page of object metadata. Cursor is
// opaque to callers and must be returned unchanged from a prior ListPage.
type ListRequest struct {
	Prefix string
	Cursor string
	Limit  int
}

// ListPage contains one key-ordered page. NextCursor is empty only on the
// final page.
type ListPage struct {
	Objects    []Object
	NextCursor string
}

// Backend is the smallest contract required by upload delivery, cleanup, and
// orphan reconciliation. Implementations must treat Put of an existing key as
// an atomic replacement, Delete of a missing key as a successful no-op, and
// List pages as key-ordered, unique, restricted to the requested prefix, no
// larger than the requested limit, and carrying an authoritative non-zero
// ModifiedAt timestamp. NextCursor must advance and remain stable for the
// duration of one listing. Ridu still revalidates object ownership and
// committed upload references immediately before destructive reconciliation.
type Backend interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Open(context.Context, string) (io.ReadCloser, Object, error)
	Delete(context.Context, string) error
	List(context.Context, ListRequest) (ListPage, error)
}

// HealthBackend is implemented by production backends that can verify their
// required dependency without reading or mutating an application object.
// Ridu includes it in /readyz when available; custom backends should keep the
// check bounded by Context and safe to call repeatedly.
type HealthBackend interface {
	Ping(context.Context) error
}

// URLSigner is optional. Private delivery remains mediated by Ridu; this is
// for explicit short-lived direct-download flows.
type URLSigner interface {
	SignedURL(context.Context, string, time.Duration) (string, error)
}
