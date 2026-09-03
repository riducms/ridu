package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func (backend *Store) EnqueueTask(ctx context.Context, task store.Task) (store.Task, error) {
	if err := store.ValidateTaskAdmission(task); err != nil {
		return store.Task{}, err
	}
	if err := validateSQLiteTimes("task run time", task.RunAt); err != nil {
		return store.Task{}, err
	}
	id, err := newID("task")
	if err != nil {
		return store.Task{}, err
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

	err = backend.withImmediate(ctx, func(connection *sql.Conn) error {
		if err := sqliteTaskReferencesExist(ctx, connection, task.Target, task.RequestedBy); err != nil {
			return err
		}
		now := backend.now().UTC()
		if err := validateSQLiteTimes("task timestamp", now); err != nil {
			return err
		}
		_, err := connection.ExecContext(ctx, `INSERT INTO ridu_tasks (
  id, slug, queue, concurrency_key, input_json, state, run_at, attempts,
  max_attempts, retry_delay_ns, max_retry_delay_ns, backoff, timeout_ns, retention_ns,
  target_collection_id, target_document_id, requested_by_collection_id, requested_by_document_id,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, 'queued', ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			task.ID, task.Slug, task.Queue, task.ConcurrencyKey, string(task.Input), encodeTime(task.RunAt),
			task.MaxAttempts, int64(task.RetryDelay), int64(task.MaxRetryDelay), string(task.Backoff),
			int64(task.Timeout), int64(task.Retention), sqliteTaskReferenceCollection(task.Target),
			sqliteTaskReferenceDocument(task.Target), sqliteTaskReferenceCollection(task.RequestedBy),
			sqliteTaskReferenceDocument(task.RequestedBy), encodeTime(now), encodeTime(now),
		)
		if err != nil {
			return translateError(err)
		}
		task.CreatedAt = now
		task.UpdatedAt = now
		return nil
	})
	if err != nil {
		return store.Task{}, err
	}
	return task, nil
}

func (backend *Store) FindTask(ctx context.Context, id string) (store.Task, error) {
	task, err := sqliteScanTask(backend.db.QueryRowContext(ctx,
		"SELECT "+sqliteTaskColumns+" FROM ridu_tasks WHERE id = ?", id))
	return task, translateError(err)
}

func (backend *Store) ListTasks(ctx context.Context, request store.TaskList) ([]store.Task, error) {
	if err := store.ValidateTaskList(request); err != nil {
		return nil, err
	}
	predicates := make([]string, 0, 3)
	arguments := make([]any, 0, 8)
	if request.Slug != "" {
		predicates = append(predicates, "slug = ?")
		arguments = append(arguments, request.Slug)
	}
	if request.Target != nil {
		predicates = append(predicates, "target_collection_id = ? AND target_document_id = ?")
		arguments = append(arguments, string(request.Target.CollectionID), request.Target.DocumentID)
	}
	if len(request.States) != 0 {
		predicates = append(predicates, sqliteTaskInPredicate("state", len(request.States)))
		for _, state := range request.States {
			arguments = append(arguments, string(state))
		}
	}
	where := "1 = 1"
	if len(predicates) != 0 {
		where = strings.Join(predicates, " AND ")
	}
	arguments = append(arguments, request.Limit)
	rows, err := backend.db.QueryContext(ctx, fmt.Sprintf(
		"SELECT %s FROM ridu_tasks WHERE %s ORDER BY run_at, created_at, id LIMIT ?",
		sqliteTaskColumns, where,
	), arguments...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	result := make([]store.Task, 0)
	for rows.Next() {
		task, err := sqliteScanTask(rows)
		if err != nil {
			return nil, translateError(err)
		}
		result = append(result, task)
	}
	return result, translateError(rows.Err())
}

func (backend *Store) CancelTask(ctx context.Context, id string) error {
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		nowTime := backend.now().UTC()
		if err := validateSQLiteTimes("task timestamp", nowTime); err != nil {
			return err
		}
		now := encodeTime(nowTime)
		var retention int64
		if err := connection.QueryRowContext(ctx, `SELECT retention_ns FROM ridu_tasks
WHERE id = ? AND state IN ('queued', 'running')`, id).Scan(&retention); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return store.ErrNotFound
			}
			return translateError(err)
		}
		retainUntil := nowTime.Add(time.Duration(retention))
		if err := validateSQLiteTimes("task retention timestamp", retainUntil); err != nil {
			return err
		}
		result, err := connection.ExecContext(ctx, `UPDATE ridu_tasks SET
  state = 'canceled', lease_token = '', lease_expires_at = NULL,
  updated_at = ?, completed_at = ?, retain_until = ?
WHERE id = ? AND state IN ('queued', 'running')`, now, now, encodeTime(retainUntil), id)
		return sqliteTaskMutationResult(result, err, store.ErrNotFound)
	})
}

func (backend *Store) DismissTaskForTarget(ctx context.Context, id, slug string, target store.DocumentReference) error {
	if err := store.ValidateTaskList(store.TaskList{Slug: slug, Target: &target, Limit: 1}); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		result, err := connection.ExecContext(ctx, `DELETE FROM ridu_tasks
WHERE id = ? AND slug = ? AND target_collection_id = ? AND target_document_id = ?
  AND state IN ('queued', 'running', 'failed', 'canceled')`,
			id, slug, string(target.CollectionID), target.DocumentID)
		return sqliteTaskMutationResult(result, err, store.ErrNotFound)
	})
}

func (backend *Store) ClaimTasks(ctx context.Context, request store.TaskClaim) ([]store.Task, error) {
	if err := store.ValidateTaskClaim(request); err != nil {
		return nil, err
	}
	claimed := make([]store.Task, 0, request.Limit)
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		now := backend.now().UTC()
		if err := validateSQLiteTimes("task timestamp", now, now.Add(request.LeaseDuration)); err != nil {
			return err
		}
		encodedNow := encodeTime(now)
		predicates := []string{
			"((candidate.state = 'queued' AND candidate.run_at <= ?) OR (candidate.state = 'running' AND candidate.lease_expires_at <= ?))",
		}
		arguments := []any{encodedNow, encodedNow}
		if len(request.Queues) != 0 {
			predicates = append(predicates, sqliteTaskInPredicate("candidate.queue", len(request.Queues)))
			for _, queue := range request.Queues {
				arguments = append(arguments, queue)
			}
		}
		if len(request.Slugs) != 0 {
			predicates = append(predicates, sqliteTaskInPredicate("candidate.slug", len(request.Slugs)))
			for _, slug := range request.Slugs {
				arguments = append(arguments, slug)
			}
		}
		arguments = append(arguments, encodedNow, request.Limit)
		statement := fmt.Sprintf(`WITH ranked AS (
  SELECT candidate.id, row_number() OVER (
    PARTITION BY candidate.queue, candidate.concurrency_key = '',
      CASE WHEN candidate.concurrency_key = '' THEN candidate.id ELSE candidate.concurrency_key END
    ORDER BY candidate.run_at, candidate.created_at, candidate.id
  ) AS concurrency_rank
  FROM ridu_tasks AS candidate
  WHERE %s
    AND (candidate.concurrency_key = '' OR NOT EXISTS (
      SELECT 1 FROM ridu_tasks AS active
      WHERE active.id <> candidate.id AND active.queue = candidate.queue
        AND active.concurrency_key = candidate.concurrency_key
        AND active.state = 'running' AND active.lease_expires_at > ?
    ))
)
SELECT id FROM ridu_tasks
WHERE id IN (SELECT id FROM ranked WHERE concurrency_rank = 1)
ORDER BY run_at, created_at, id
LIMIT ?`, strings.Join(predicates, " AND "))
		rows, err := connection.QueryContext(ctx, statement, arguments...)
		if err != nil {
			return translateError(err)
		}
		candidateIDs := make([]string, 0, request.Limit)
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return translateError(err)
			}
			candidateIDs = append(candidateIDs, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return translateError(err)
		}
		rows.Close()

		for _, id := range candidateIDs {
			leaseToken, err := newID("lease")
			if err != nil {
				return err
			}
			expiresAt := encodeTime(now.Add(request.LeaseDuration))
			claimedTask, err := sqliteScanTask(connection.QueryRowContext(ctx, `UPDATE ridu_tasks SET
  state = 'running', attempts = attempts + 1, lease_token = ?,
  lease_expires_at = ?, updated_at = ?
WHERE id = ? AND ((state = 'queued' AND run_at <= ?) OR (state = 'running' AND lease_expires_at <= ?))
RETURNING `+sqliteTaskColumns,
				leaseToken, expiresAt, encodedNow, id, encodedNow, encodedNow,
			))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return translateError(err)
			}
			claimed = append(claimed, claimedTask)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (backend *Store) HeartbeatTask(ctx context.Context, id, leaseToken string, leaseDuration time.Duration) error {
	if err := store.ValidateTaskLeaseDuration(leaseDuration); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		now := backend.now().UTC()
		if err := validateSQLiteTimes("task timestamp", now, now.Add(leaseDuration)); err != nil {
			return err
		}
		result, err := connection.ExecContext(ctx, `UPDATE ridu_tasks
SET lease_expires_at = ?, updated_at = ?
WHERE id = ? AND state = 'running' AND lease_token = ? AND lease_expires_at > ?`,
			encodeTime(now.Add(leaseDuration)), encodeTime(now), id, leaseToken, encodeTime(now))
		return sqliteTaskMutationResult(result, err, store.ErrTaskLeaseLost)
	})
}

func (backend *Store) CompleteTask(ctx context.Context, id, leaseToken string, output json.RawMessage) error {
	if err := store.ValidateTaskPayload(output); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		nowTime := backend.now().UTC()
		if err := validateSQLiteTimes("task timestamp", nowTime); err != nil {
			return err
		}
		now := encodeTime(nowTime)
		var retention int64
		if err := connection.QueryRowContext(ctx, `SELECT retention_ns FROM ridu_tasks
WHERE id = ? AND state = 'running' AND lease_token = ? AND lease_expires_at > ?`,
			id, leaseToken, now).Scan(&retention); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return store.ErrTaskLeaseLost
			}
			return translateError(err)
		}
		retainUntil := nowTime.Add(time.Duration(retention))
		if err := validateSQLiteTimes("task retention timestamp", retainUntil); err != nil {
			return err
		}
		result, err := connection.ExecContext(ctx, `UPDATE ridu_tasks SET
  state = 'succeeded', output_json = ?, lease_token = '', lease_expires_at = NULL,
  last_error_code = '', last_error = '', updated_at = ?, completed_at = ?,
  retain_until = ?
WHERE id = ? AND state = 'running' AND lease_token = ? AND lease_expires_at > ?`,
			string(output), now, now, encodeTime(retainUntil), id, leaseToken, now)
		return sqliteTaskMutationResult(result, err, store.ErrTaskLeaseLost)
	})
}

func (backend *Store) FailTask(ctx context.Context, failure store.TaskFailure) error {
	if err := store.ValidateTaskFailure(failure); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		nowTime := backend.now().UTC()
		timestamps := []time.Time{nowTime}
		if failure.RetryAfter != nil {
			timestamps = append(timestamps, nowTime.Add(*failure.RetryAfter))
		}
		if err := validateSQLiteTimes("task timestamp", timestamps...); err != nil {
			return err
		}
		now := encodeTime(nowTime)
		var result sql.Result
		var err error
		if failure.RetryAfter != nil {
			result, err = connection.ExecContext(ctx, `UPDATE ridu_tasks SET
  state = 'queued', run_at = ? + ?, lease_token = '', lease_expires_at = NULL,
  last_error_code = ?, last_error = ?, updated_at = ?, completed_at = NULL, retain_until = NULL
WHERE id = ? AND state = 'running' AND lease_token = ? AND lease_expires_at > ?`,
				now, int64(*failure.RetryAfter), failure.Code, failure.Message, now,
				failure.ID, failure.LeaseToken, now)
		} else {
			var retention int64
			if err := connection.QueryRowContext(ctx, `SELECT retention_ns FROM ridu_tasks
WHERE id = ? AND state = 'running' AND lease_token = ? AND lease_expires_at > ?`,
				failure.ID, failure.LeaseToken, now).Scan(&retention); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return store.ErrTaskLeaseLost
				}
				return translateError(err)
			}
			retainUntil := nowTime.Add(time.Duration(retention))
			if err := validateSQLiteTimes("task retention timestamp", retainUntil); err != nil {
				return err
			}
			result, err = connection.ExecContext(ctx, `UPDATE ridu_tasks SET
  state = 'failed', lease_token = '', lease_expires_at = NULL,
  last_error_code = ?, last_error = ?, updated_at = ?, completed_at = ?,
  retain_until = ?
WHERE id = ? AND state = 'running' AND lease_token = ? AND lease_expires_at > ?`,
				failure.Code, failure.Message, now, now, encodeTime(retainUntil), failure.ID, failure.LeaseToken, now)
		}
		return sqliteTaskMutationResult(result, err, store.ErrTaskLeaseLost)
	})
}

func (backend *Store) ReleaseTask(ctx context.Context, id, leaseToken string, delay time.Duration, code, message string) error {
	if err := store.ValidateTaskRelease(delay, code, message); err != nil {
		return err
	}
	return backend.withImmediate(ctx, func(connection *sql.Conn) error {
		nowTime := backend.now().UTC()
		if err := validateSQLiteTimes("task timestamp", nowTime, nowTime.Add(delay)); err != nil {
			return err
		}
		now := encodeTime(nowTime)
		result, err := connection.ExecContext(ctx, `UPDATE ridu_tasks SET
  state = 'queued', run_at = ? + ?, lease_token = '', lease_expires_at = NULL,
  last_error_code = ?, last_error = ?, updated_at = ?, completed_at = NULL, retain_until = NULL
WHERE id = ? AND state = 'running' AND lease_token = ? AND lease_expires_at > ?`,
			now, int64(delay), code, message, now, id, leaseToken, now)
		return sqliteTaskMutationResult(result, err, store.ErrTaskLeaseLost)
	})
}

func (backend *Store) PruneTasks(ctx context.Context, limit int) (int, error) {
	if err := store.ValidateTaskBatch(limit); err != nil {
		return 0, err
	}
	pruned := 0
	err := backend.withImmediate(ctx, func(connection *sql.Conn) error {
		now := backend.now().UTC()
		if err := validateSQLiteTimes("task timestamp", now); err != nil {
			return err
		}
		result, err := connection.ExecContext(ctx, `DELETE FROM ridu_tasks WHERE id IN (
  SELECT id FROM ridu_tasks
  WHERE retain_until IS NOT NULL AND retain_until <= ?
  ORDER BY retain_until, id
  LIMIT ?
)`, encodeTime(now), limit)
		if err != nil {
			return translateError(err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return translateError(err)
		}
		pruned = int(rows)
		return nil
	})
	return pruned, err
}

func sqliteTaskReferencesExist(ctx context.Context, runner sqlRunner, references ...*store.DocumentReference) error {
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if reference == nil {
			continue
		}
		key := string(reference.CollectionID) + "\x00" + reference.DocumentID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		var marker int
		err := runner.QueryRowContext(ctx, `SELECT 1 FROM ridu_documents
WHERE collection_id = ? AND id = ? AND deleted_at IS NULL`,
			string(reference.CollectionID), reference.DocumentID).Scan(&marker)
		if err != nil {
			return translateError(err)
		}
	}
	return nil
}

func sqliteTaskMutationResult(result sql.Result, err error, missing error) error {
	if err != nil {
		return translateError(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return translateError(err)
	}
	if rows == 0 {
		return missing
	}
	return nil
}

func sqliteTaskInPredicate(column string, count int) string {
	placeholders := make([]string, count)
	for index := range placeholders {
		placeholders[index] = "?"
	}
	return column + " IN (" + strings.Join(placeholders, ", ") + ")"
}

const sqliteTaskColumns = `id, slug, queue, concurrency_key, input_json, output_json,
state, run_at, attempts, max_attempts, retry_delay_ns, max_retry_delay_ns, backoff,
timeout_ns, retention_ns, lease_token, lease_expires_at, target_collection_id,
target_document_id, requested_by_collection_id, requested_by_document_id,
last_error_code, last_error, created_at, updated_at, completed_at, retain_until`

type sqliteTaskScanner interface{ Scan(...any) error }

func sqliteScanTask(scanner sqliteTaskScanner) (store.Task, error) {
	var task store.Task
	var input string
	var output sql.NullString
	var state, backoff string
	var runAt, retryDelay, maxRetryDelay, timeout, retention, createdAt, updatedAt int64
	var leaseExpiresAt, completedAt, retainUntil sql.NullInt64
	var targetCollection, targetDocument, requestedCollection, requestedDocument sql.NullString
	err := scanner.Scan(
		&task.ID, &task.Slug, &task.Queue, &task.ConcurrencyKey, &input, &output,
		&state, &runAt, &task.Attempts, &task.MaxAttempts, &retryDelay, &maxRetryDelay,
		&backoff, &timeout, &retention, &task.LeaseToken, &leaseExpiresAt,
		&targetCollection, &targetDocument, &requestedCollection, &requestedDocument,
		&task.LastErrorCode, &task.LastError, &createdAt, &updatedAt, &completedAt, &retainUntil,
	)
	if err != nil {
		return store.Task{}, err
	}
	task.Input = append(json.RawMessage(nil), input...)
	if output.Valid {
		task.Output = append(json.RawMessage(nil), output.String...)
	}
	task.State = store.TaskState(state)
	task.RunAt = decodeTime(runAt)
	task.RetryDelay = time.Duration(retryDelay)
	task.MaxRetryDelay = time.Duration(maxRetryDelay)
	task.Backoff = store.TaskBackoff(backoff)
	task.Timeout = time.Duration(timeout)
	task.Retention = time.Duration(retention)
	task.LeaseExpiresAt = timePointer(leaseExpiresAt)
	if targetCollection.Valid && targetDocument.Valid {
		task.Target = &store.DocumentReference{
			CollectionID: schema.StableID(targetCollection.String),
			DocumentID:   targetDocument.String,
		}
	}
	if requestedCollection.Valid && requestedDocument.Valid {
		task.RequestedBy = &store.DocumentReference{
			CollectionID: schema.StableID(requestedCollection.String),
			DocumentID:   requestedDocument.String,
		}
	}
	task.CreatedAt = decodeTime(createdAt)
	task.UpdatedAt = decodeTime(updatedAt)
	task.CompletedAt = timePointer(completedAt)
	task.RetainUntil = timePointer(retainUntil)
	return task, nil
}

func sqliteTaskReferenceCollection(reference *store.DocumentReference) any {
	if reference == nil {
		return nil
	}
	return string(reference.CollectionID)
}

func sqliteTaskReferenceDocument(reference *store.DocumentReference) any {
	if reference == nil {
		return nil
	}
	return reference.DocumentID
}

var _ store.TaskStore = (*Store)(nil)
