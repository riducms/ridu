package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (backend *Store) EnqueueTask(ctx context.Context, task store.Task) (store.Task, error) {
	if err := store.ValidateTaskAdmission(task); err != nil {
		return store.Task{}, err
	}
	id, err := newID("task")
	if err != nil {
		return store.Task{}, err
	}
	transaction, err := backend.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return store.Task{}, translateError(err)
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	references := make([]store.DocumentReference, 0, 2)
	if task.Target != nil {
		references = append(references, *task.Target)
	}
	if task.RequestedBy != nil {
		references = append(references, *task.RequestedBy)
	}
	if len(references) != 0 {
		if err := lockDocumentReferences(ctx, transaction, references...); err != nil {
			return store.Task{}, err
		}
	}
	task.ID = id
	task.State = store.TaskStateQueued
	task.Attempts = 0
	task.Output = nil
	task.LeaseToken = ""
	task.LeaseExpiresAt = nil
	task.LastErrorCode = ""
	task.LastError = ""
	task.CompletedAt = nil
	task.RetainUntil = nil
	task.RunAt = task.RunAt.UTC()
	task.Input = append(json.RawMessage(nil), task.Input...)
	statement := `INSERT INTO ridu_tasks (
  id, task_slug, queue, concurrency_key, input, state, run_at, attempts,
  max_attempts, retry_delay_ms, max_retry_delay_ms, backoff, timeout_ms, retention_ms,
  target_collection_id, target_document_id, requested_by_collection_id, requested_by_document_id
) VALUES ($1, $2, $3, $4, $5, 'queued', $6, 0, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
RETURNING created_at, updated_at`
	if err := transaction.QueryRow(ctx, statement,
		task.ID, task.Slug, task.Queue, nullableString(task.ConcurrencyKey), task.Input, task.RunAt,
		task.MaxAttempts, task.RetryDelay.Milliseconds(), task.MaxRetryDelay.Milliseconds(), task.Backoff,
		task.Timeout.Milliseconds(), task.Retention.Milliseconds(), referenceCollection(task.Target), referenceDocument(task.Target),
		referenceCollection(task.RequestedBy), referenceDocument(task.RequestedBy),
	).Scan(&task.CreatedAt, &task.UpdatedAt); err != nil {
		return store.Task{}, translateError(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return store.Task{}, translateError(err)
	}
	return task, nil
}

func (backend *Store) FindTask(ctx context.Context, id string) (store.Task, error) {
	statement := "SELECT " + taskColumns() + " FROM ridu_tasks WHERE id = $1"
	task, err := scanTask(backend.pool.QueryRow(ctx, statement, id))
	return task, translateError(err)
}

func (backend *Store) ListTasks(ctx context.Context, request store.TaskList) ([]store.Task, error) {
	if err := store.ValidateTaskList(request); err != nil {
		return nil, err
	}
	predicates := make([]string, 0, 4)
	arguments := make([]any, 0, 6)
	add := func(predicate string, values ...any) {
		predicates = append(predicates, predicate)
		arguments = append(arguments, values...)
	}
	if request.Slug != "" {
		add(fmt.Sprintf("task_slug = $%d", len(arguments)+1), request.Slug)
	}
	if request.Target != nil {
		add(fmt.Sprintf("target_collection_id = $%d AND target_document_id = $%d", len(arguments)+1, len(arguments)+2), request.Target.CollectionID, request.Target.DocumentID)
	}
	if len(request.States) != 0 {
		states := make([]string, len(request.States))
		for index, state := range request.States {
			states[index] = string(state)
		}
		add(fmt.Sprintf("state = ANY($%d::text[])", len(arguments)+1), states)
	}
	where := "TRUE"
	if len(predicates) != 0 {
		where = strings.Join(predicates, " AND ")
	}
	arguments = append(arguments, request.Limit)
	statement := fmt.Sprintf("SELECT %s FROM ridu_tasks WHERE %s ORDER BY run_at, created_at, id LIMIT $%d", taskColumns(), where, len(arguments))
	rows, err := backend.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	result := make([]store.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, translateError(err)
		}
		result = append(result, task)
	}
	return result, translateError(rows.Err())
}

func (backend *Store) CancelTask(ctx context.Context, id string) error {
	return backend.cancelTask(ctx, id, "", nil)
}

func (backend *Store) DismissTaskForTarget(ctx context.Context, id, slug string, target store.DocumentReference) error {
	if err := store.ValidateTaskList(store.TaskList{Slug: slug, Target: &target, Limit: 1}); err != nil {
		return err
	}
	tag, err := backend.pool.Exec(ctx, `DELETE FROM ridu_tasks
WHERE id = $1 AND task_slug = $2 AND target_collection_id = $3 AND target_document_id = $4
  AND state IN ('queued', 'running', 'failed', 'canceled')`, id, slug, target.CollectionID, target.DocumentID)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (backend *Store) cancelTask(ctx context.Context, id, slug string, target *store.DocumentReference) error {
	arguments := []any{id}
	predicate := "id = $1"
	if slug != "" {
		arguments = append(arguments, slug)
		predicate += fmt.Sprintf(" AND task_slug = $%d", len(arguments))
	}
	if target != nil {
		arguments = append(arguments, target.CollectionID, target.DocumentID)
		predicate += fmt.Sprintf(" AND target_collection_id = $%d AND target_document_id = $%d", len(arguments)-1, len(arguments))
	}
	statement := `UPDATE ridu_tasks SET
  state = 'canceled', lease_token = NULL, lease_expires_at = NULL,
  updated_at = now(), completed_at = now(),
  retain_until = now() + retention_ms * interval '1 millisecond'
WHERE ` + predicate + ` AND state IN ('queued', 'running')`
	tag, err := backend.pool.Exec(ctx, statement, arguments...)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (backend *Store) ClaimTasks(ctx context.Context, request store.TaskClaim) ([]store.Task, error) {
	if err := store.ValidateTaskClaim(request); err != nil {
		return nil, err
	}
	transaction, err := backend.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, translateError(err)
	}
	defer func() { _ = rollbackPostgresTransaction(ctx, transaction) }()
	predicates := []string{"((candidate.state = 'queued' AND candidate.run_at <= now()) OR (candidate.state = 'running' AND candidate.lease_expires_at <= now()))"}
	arguments := make([]any, 0, 3)
	if len(request.Queues) != 0 {
		arguments = append(arguments, request.Queues)
		predicates = append(predicates, fmt.Sprintf("candidate.queue = ANY($%d::text[])", len(arguments)))
	}
	if len(request.Slugs) != 0 {
		arguments = append(arguments, request.Slugs)
		predicates = append(predicates, fmt.Sprintf("candidate.task_slug = ANY($%d::text[])", len(arguments)))
	}
	arguments = append(arguments, request.Limit)
	statement := fmt.Sprintf(`WITH ranked AS (
  SELECT candidate.id, row_number() OVER (
    PARTITION BY candidate.queue, candidate.concurrency_key IS NULL, COALESCE(candidate.concurrency_key, candidate.id)
    ORDER BY candidate.run_at, candidate.created_at, candidate.id
  ) AS concurrency_rank
  FROM ridu_tasks AS candidate
  WHERE %s
    AND (candidate.concurrency_key IS NULL OR NOT EXISTS (
      SELECT 1 FROM ridu_tasks AS active
      WHERE active.id <> candidate.id AND active.queue = candidate.queue
        AND active.concurrency_key = candidate.concurrency_key
        AND active.state = 'running' AND active.lease_expires_at > now()
    ))
)
SELECT %s FROM ridu_tasks
WHERE id IN (SELECT id FROM ranked WHERE concurrency_rank = 1)
ORDER BY run_at, created_at, id
FOR UPDATE SKIP LOCKED LIMIT $%d`, strings.Join(predicates, " AND "), taskColumns(), len(arguments))
	rows, err := transaction.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, translateError(err)
	}
	candidates := make([]store.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, translateError(err)
		}
		candidates = append(candidates, task)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, translateError(err)
	}
	rows.Close()
	claimed := make([]store.Task, 0, request.Limit)
	for _, candidate := range candidates {
		if len(claimed) >= request.Limit {
			break
		}
		if candidate.ConcurrencyKey != "" {
			key := fmt.Sprintf("%d:%s%s", len(candidate.Queue), candidate.Queue, candidate.ConcurrencyKey)
			var locked bool
			if err := transaction.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))", key).Scan(&locked); err != nil {
				return nil, translateError(err)
			}
			if !locked {
				continue
			}
			var occupied bool
			if err := transaction.QueryRow(ctx, `SELECT EXISTS (
  SELECT 1 FROM ridu_tasks
  WHERE id <> $1 AND queue = $2 AND concurrency_key = $3
    AND state = 'running' AND lease_expires_at > now()
)`, candidate.ID, candidate.Queue, candidate.ConcurrencyKey).Scan(&occupied); err != nil {
				return nil, translateError(err)
			}
			if occupied {
				continue
			}
		}
		leaseToken, err := newID("lease")
		if err != nil {
			return nil, err
		}
		statement := `UPDATE ridu_tasks SET
  state = 'running', attempts = attempts + 1, lease_token = $2,
  lease_expires_at = now() + $3 * interval '1 millisecond', updated_at = now()
WHERE id = $1 AND ((state = 'queued' AND run_at <= now()) OR (state = 'running' AND lease_expires_at <= now()))
RETURNING ` + taskColumns()
		claimedTask, err := scanTask(transaction.QueryRow(ctx, statement, candidate.ID, leaseToken, request.LeaseDuration.Milliseconds()))
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, translateError(err)
		}
		claimed = append(claimed, claimedTask)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, translateError(err)
	}
	return claimed, nil
}

func (backend *Store) HeartbeatTask(ctx context.Context, id, leaseToken string, leaseDuration time.Duration) error {
	if err := store.ValidateTaskLeaseDuration(leaseDuration); err != nil {
		return err
	}
	tag, err := backend.pool.Exec(ctx, `UPDATE ridu_tasks
SET lease_expires_at = now() + $3 * interval '1 millisecond', updated_at = now()
WHERE id = $1 AND state = 'running' AND lease_token = $2 AND lease_expires_at > now()`, id, leaseToken, leaseDuration.Milliseconds())
	return taskLeaseMutationResult(tag.RowsAffected(), err)
}

func (backend *Store) CompleteTask(ctx context.Context, id, leaseToken string, output json.RawMessage) error {
	if err := store.ValidateTaskPayload(output); err != nil {
		return err
	}
	tag, err := backend.pool.Exec(ctx, `UPDATE ridu_tasks SET
  state = 'succeeded', output = $3, lease_token = NULL, lease_expires_at = NULL,
  last_error_code = NULL, last_error = NULL, updated_at = now(), completed_at = now(),
  retain_until = now() + retention_ms * interval '1 millisecond'
WHERE id = $1 AND state = 'running' AND lease_token = $2 AND lease_expires_at > now()`, id, leaseToken, output)
	return taskLeaseMutationResult(tag.RowsAffected(), err)
}

func (backend *Store) FailTask(ctx context.Context, failure store.TaskFailure) error {
	if err := store.ValidateTaskFailure(failure); err != nil {
		return err
	}
	var statement string
	var arguments []any
	if failure.RetryAfter != nil {
		statement = `UPDATE ridu_tasks SET
	  state = 'queued', run_at = now() + $5 * interval '1 millisecond', lease_token = NULL, lease_expires_at = NULL,
	  last_error_code = $3, last_error = $4, updated_at = now(), completed_at = NULL, retain_until = NULL
	WHERE id = $1 AND state = 'running' AND lease_token = $2 AND lease_expires_at > now()`
		arguments = []any{failure.ID, failure.LeaseToken, nullableString(failure.Code), nullableString(failure.Message), failure.RetryAfter.Milliseconds()}
	} else {
		statement = `UPDATE ridu_tasks SET
  state = 'failed', lease_token = NULL, lease_expires_at = NULL,
  last_error_code = $3, last_error = $4, updated_at = now(), completed_at = now(),
  retain_until = now() + retention_ms * interval '1 millisecond'
	WHERE id = $1 AND state = 'running' AND lease_token = $2 AND lease_expires_at > now()`
		arguments = []any{failure.ID, failure.LeaseToken, nullableString(failure.Code), nullableString(failure.Message)}
	}
	tag, err := backend.pool.Exec(ctx, statement, arguments...)
	return taskLeaseMutationResult(tag.RowsAffected(), err)
}

func (backend *Store) ReleaseTask(ctx context.Context, id, leaseToken string, delay time.Duration, code, message string) error {
	if err := store.ValidateTaskRelease(delay, code, message); err != nil {
		return err
	}
	tag, err := backend.pool.Exec(ctx, `UPDATE ridu_tasks SET
	  state = 'queued', run_at = now() + $3 * interval '1 millisecond', lease_token = NULL, lease_expires_at = NULL,
	  last_error_code = $4, last_error = $5, updated_at = now(), completed_at = NULL, retain_until = NULL
	WHERE id = $1 AND state = 'running' AND lease_token = $2 AND lease_expires_at > now()`, id, leaseToken, delay.Milliseconds(), nullableString(code), nullableString(message))
	return taskLeaseMutationResult(tag.RowsAffected(), err)
}

func (backend *Store) PruneTasks(ctx context.Context, limit int) (int, error) {
	if err := store.ValidateTaskBatch(limit); err != nil {
		return 0, err
	}
	tag, err := backend.pool.Exec(ctx, `DELETE FROM ridu_tasks WHERE id IN (
  SELECT id FROM ridu_tasks
  WHERE retain_until IS NOT NULL AND retain_until <= now()
  ORDER BY retain_until, id
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)`, limit)
	if err != nil {
		return 0, translateError(err)
	}
	return int(tag.RowsAffected()), nil
}

func taskLeaseMutationResult(rows int64, err error) error {
	if err != nil {
		return translateError(err)
	}
	if rows == 0 {
		return store.ErrTaskLeaseLost
	}
	return nil
}

func taskColumns() string {
	return strings.Join([]string{
		"id", "task_slug", "queue", "COALESCE(concurrency_key, '')", "input", "output", "state", "run_at", "attempts", "max_attempts",
		"retry_delay_ms", "max_retry_delay_ms", "backoff", "timeout_ms", "retention_ms", "COALESCE(lease_token, '')", "lease_expires_at",
		"target_collection_id", "target_document_id", "requested_by_collection_id", "requested_by_document_id",
		"COALESCE(last_error_code, '')", "COALESCE(last_error, '')", "created_at", "updated_at", "completed_at", "retain_until",
	}, ", ")
}

type taskScanner interface{ Scan(...any) error }

func scanTask(scanner taskScanner) (store.Task, error) {
	var task store.Task
	var state, backoff string
	var retryDelayMillis, maxRetryDelayMillis, timeoutMillis, retentionMillis int64
	var targetCollection, targetDocument, requestedCollection, requestedDocument *string
	err := scanner.Scan(
		&task.ID, &task.Slug, &task.Queue, &task.ConcurrencyKey, &task.Input, &task.Output, &state, &task.RunAt, &task.Attempts, &task.MaxAttempts,
		&retryDelayMillis, &maxRetryDelayMillis, &backoff, &timeoutMillis, &retentionMillis, &task.LeaseToken, &task.LeaseExpiresAt,
		&targetCollection, &targetDocument, &requestedCollection, &requestedDocument,
		&task.LastErrorCode, &task.LastError, &task.CreatedAt, &task.UpdatedAt, &task.CompletedAt, &task.RetainUntil,
	)
	if err != nil {
		return store.Task{}, err
	}
	task.State = store.TaskState(state)
	task.Backoff = store.TaskBackoff(backoff)
	task.RetryDelay = time.Duration(retryDelayMillis) * time.Millisecond
	task.MaxRetryDelay = time.Duration(maxRetryDelayMillis) * time.Millisecond
	task.Timeout = time.Duration(timeoutMillis) * time.Millisecond
	task.Retention = time.Duration(retentionMillis) * time.Millisecond
	if targetCollection != nil && targetDocument != nil {
		task.Target = &store.DocumentReference{CollectionID: schema.StableID(*targetCollection), DocumentID: *targetDocument}
	}
	if requestedCollection != nil && requestedDocument != nil {
		task.RequestedBy = &store.DocumentReference{CollectionID: schema.StableID(*requestedCollection), DocumentID: *requestedDocument}
	}
	return task, nil
}

func referenceCollection(reference *store.DocumentReference) any {
	if reference == nil {
		return nil
	}
	return string(reference.CollectionID)
}

func referenceDocument(reference *store.DocumentReference) any {
	if reference == nil {
		return nil
	}
	return reference.DocumentID
}

// taskClaimOrder is used by unit tests to prove the SQL and in-memory stores
// share deterministic due ordering.
func taskClaimOrder(tasks []store.Task) {
	sort.Slice(tasks, func(left, right int) bool {
		if tasks[left].RunAt.Equal(tasks[right].RunAt) {
			if tasks[left].CreatedAt.Equal(tasks[right].CreatedAt) {
				return tasks[left].ID < tasks[right].ID
			}
			return tasks[left].CreatedAt.Before(tasks[right].CreatedAt)
		}
		return tasks[left].RunAt.Before(tasks[right].RunAt)
	})
}

var _ store.TaskStore = (*Store)(nil)
