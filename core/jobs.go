package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

const (
	JobStatusPending   = "pending"
	JobStatusRunning   = "running"
	JobStatusCompleted = "completed"
	JobStatusFailed    = "failed"
	JobStatusDead      = "dead"

	OutboxStatusPending   = "pending"
	OutboxStatusPublished = "published"
	OutboxStatusFailed    = "failed"
)

// Job represents a durable background job stored in PostgreSQL.
type Job struct {
	Id             string             `db:"id" json:"id"`
	Queue          string             `db:"queue" json:"queue"`
	Task           string             `db:"task" json:"task"`
	Payload        types.JSONMap[any] `db:"payload" json:"payload"`
	Status         string             `db:"status" json:"status"`
	Attempts       int                `db:"attempts" json:"attempts"`
	MaxRetries     int                `db:"max_retries" json:"maxRetries"`
	BackoffSeconds int                `db:"backoff_seconds" json:"backoffSeconds"`
	IdempotencyKey string             `db:"idempotency_key" json:"idempotencyKey,omitempty"`
	LastError      string             `db:"last_error" json:"lastError,omitempty"`
	Result         types.JSONMap[any] `db:"result" json:"result,omitempty"`
	RunAt          types.DateTime     `db:"run_at" json:"runAt"`
	LeasedAt       types.DateTime     `db:"leased_at" json:"leasedAt,omitempty"`
	LeasedUntil    types.DateTime     `db:"leased_until" json:"leasedUntil,omitempty"`
	LeasedBy       string             `db:"leased_by" json:"leasedBy,omitempty"`
	Created        types.DateTime     `db:"created" json:"created"`
	Updated        types.DateTime     `db:"updated" json:"updated"`
}

// OutboxEvent represents a transactional outbox event.
type OutboxEvent struct {
	Id          string             `db:"id" json:"id"`
	Event       string             `db:"event" json:"event"`
	Topic       string             `db:"topic" json:"topic"`
	Payload     types.JSONMap[any] `db:"payload" json:"payload"`
	Status      string             `db:"status" json:"status"`
	Attempts    int                `db:"attempts" json:"attempts"`
	LastError   string             `db:"last_error" json:"lastError,omitempty"`
	ClaimToken  string             `db:"claim_token" json:"claimToken,omitempty"`
	LeasedAt    types.DateTime     `db:"leased_at" json:"leasedAt,omitempty"`
	LeasedUntil types.DateTime     `db:"leased_until" json:"leasedUntil,omitempty"`
	Created     types.DateTime     `db:"created" json:"created"`
	PublishedAt types.DateTime     `db:"published_at" json:"publishedAt,omitempty"`
}

// Webhook defines a configured webhook destination.
type Webhook struct {
	Id      string                  `db:"id" json:"id"`
	Name    string                  `db:"name" json:"name"`
	URL     string                  `db:"url" json:"url"`
	Secret  string                  `db:"secret" json:"secret"`
	Events  types.JSONArray[string] `db:"events" json:"events"`
	Active  bool                    `db:"active" json:"active"`
	Created types.DateTime          `db:"created" json:"created"`
	Updated types.DateTime          `db:"updated" json:"updated"`
}

// WebhookAttempt records a single delivery attempt to a webhook URL.
type WebhookAttempt struct {
	Id           string         `db:"id" json:"id"`
	WebhookId    string         `db:"webhook_id" json:"webhookId"`
	EventId      string         `db:"event_id" json:"eventId"`
	StatusCode   int            `db:"status_code" json:"statusCode"`
	ResponseBody string         `db:"response_body" json:"responseBody"`
	Error        string         `db:"error" json:"error"`
	DurationMs   int            `db:"duration_ms" json:"durationMs"`
	Created      types.DateTime `db:"created" json:"created"`
}

// JobHandler executes a claimed job.
type JobHandler func(ctx context.Context, job *Job) error

// OutboxProcessOptions configures transactional outbox claiming, lease lifecycle,
// batch pre-publish lease refresh, and optional background lease renewal.
type OutboxProcessOptions struct {
	// Limit is the maximum number of events claimed per batch (default 50).
	Limit int
	// LeaseDuration is the lease TTL assigned on claim and renewal (default 60s).
	LeaseDuration time.Duration
	// RenewInterval, when > 0, runs a fenced background heartbeat that extends leased_until
	// while a publisher is running. If lease ownership is lost (0 rows affected), the
	// publisher context is canceled immediately.
	RenewInterval time.Duration
	// PublishTimeout, when > 0, bounds each event's publisher execution context.
	PublishTimeout time.Duration
}

// outboxFaultHooks provides deterministic test-only fault injection and synchronization barriers.
type outboxFaultHooks struct {
	beforeClaim    func() error
	afterClaim     func(events []*OutboxEvent) error
	beforeRenew    func(evt *OutboxEvent) error
	beforeMarkPub  func(evt *OutboxEvent) error
	afterMarkPub   func(evt *OutboxEvent, affected int64) error
	beforeMarkFail func(evt *OutboxEvent) error
}

// JobsManager manages durable background jobs, leasing, worker execution, and the transactional outbox.
type JobsManager struct {
	app        App
	mu         sync.RWMutex
	handlers   map[string]JobHandler
	httpClient *http.Client
	faultHooks *outboxFaultHooks
}

func (jm *JobsManager) getOutboxFaultHooks() *outboxFaultHooks {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	return jm.faultHooks
}

// NewJobsManager creates a new JobsManager.
func NewJobsManager(app App) *JobsManager {
	return &JobsManager{
		app:      app,
		handlers: make(map[string]JobHandler),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// RegisterHandler registers a handler function for a specific job task name.
func (jm *JobsManager) RegisterHandler(task string, handler JobHandler) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.handlers[task] = handler
}

// Enqueue inserts a new job into the _jobs table atomically within the active db transaction.
func (jm *JobsManager) Enqueue(job *Job) (*Job, error) {
	if job == nil {
		return nil, errors.New("cannot enqueue nil job")
	}

	if job.Id == "" {
		job.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	}
	if job.Queue == "" {
		job.Queue = "default"
	}
	if job.Task == "" {
		return nil, errors.New("job task name is required")
	}
	if job.Status == "" {
		job.Status = JobStatusPending
	}
	if job.MaxRetries <= 0 {
		job.MaxRetries = 3
	}
	if job.BackoffSeconds <= 0 {
		job.BackoffSeconds = 10
	}
	if job.RunAt.IsZero() {
		job.RunAt = types.NowDateTime()
	}
	now := types.NowDateTime()
	job.Created = now
	job.Updated = now

	// Check idempotency key if specified
	if job.IdempotencyKey != "" {
		existing := &Job{}
		err := jm.app.DB().NewQuery(`
			SELECT [[id]], [[queue]], [[task]], [[payload]], [[status]], [[attempts]], [[max_retries]],
			       [[backoff_seconds]], [[idempotency_key]], [[last_error]], [[result]], [[run_at]],
			       [[leased_at]], [[leased_until]], [[leased_by]], [[created]], [[updated]]
			FROM {{_jobs}}
			WHERE [[idempotency_key]] = {:key}
		`).Bind(dbx.Params{"key": job.IdempotencyKey}).One(existing)

		if err == nil {
			return existing, nil // Return existing job without creating duplicate
		}
	}

	payloadBytes, _ := json.Marshal(job.Payload)

	var runAtVal any
	if !job.RunAt.IsZero() {
		runAtVal = job.RunAt.String()
	}

	insertQuery := `
		INSERT INTO {{_jobs}} (
			[[id]], [[queue]], [[task]], [[payload]], [[status]], [[attempts]], [[max_retries]],
			[[backoff_seconds]], [[idempotency_key]], [[run_at]], [[created]], [[updated]]
		) VALUES (
			{:id}, {:queue}, {:task}, {:payload}::jsonb, {:status}, {:attempts}, {:maxRetries},
			{:backoff}, {:idempotencyKey}, COALESCE({:runAt}, now()), {:created}, {:updated}
		)
	`
	if job.IdempotencyKey != "" {
		insertQuery += ` ON CONFLICT ([[idempotency_key]]) WHERE [[idempotency_key]] != '' DO NOTHING`
	}

	res, err := jm.app.DB().NewQuery(insertQuery).Bind(dbx.Params{
		"id":             job.Id,
		"queue":          job.Queue,
		"task":           job.Task,
		"payload":        string(payloadBytes),
		"status":         job.Status,
		"attempts":       job.Attempts,
		"maxRetries":     job.MaxRetries,
		"backoff":        job.BackoffSeconds,
		"idempotencyKey": job.IdempotencyKey,
		"runAt":          runAtVal,
		"created":        job.Created.String(),
		"updated":        job.Updated.String(),
	}).Execute()

	if err != nil {
		return nil, fmt.Errorf("failed to enqueue job: %w", err)
	}

	if job.IdempotencyKey != "" && res != nil {
		if affected, _ := res.RowsAffected(); affected == 0 {
			existing := &Job{}
			err := jm.app.DB().NewQuery(`
				SELECT [[id]], [[queue]], [[task]], [[payload]], [[status]], [[attempts]], [[max_retries]],
				       [[backoff_seconds]], [[idempotency_key]], [[last_error]], [[result]], [[run_at]],
				       [[leased_at]], [[leased_until]], [[leased_by]], [[created]], [[updated]]
				FROM {{_jobs}}
				WHERE [[idempotency_key]] = {:key}
			`).Bind(dbx.Params{"key": job.IdempotencyKey}).One(existing)
			if err == nil {
				return existing, nil
			}
		}
	}

	return job, nil
}

// ClaimNext atomically acquires a lease on the next pending or stale job using FOR UPDATE SKIP LOCKED.
func (jm *JobsManager) ClaimNext(queue string, workerId string, leaseDuration time.Duration) (*Job, error) {
	if queue == "" {
		queue = "default"
	}
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}
	leaseSec := int(leaseDuration.Seconds())

	job := &Job{}
	err := jm.app.DB().NewQuery(`
		UPDATE {{_jobs}}
		SET [[status]] = 'running',
		    [[leased_at]] = now(),
		    [[leased_until]] = now() + ({:leaseSec} * INTERVAL '1 second'),
		    [[leased_by]] = {:workerId},
		    [[attempts]] = [[attempts]] + 1,
		    [[updated]] = now()
		WHERE [[id]] = (
			SELECT [[id]]
			FROM {{_jobs}}
			WHERE [[queue]] = {:queue}
			  AND ([[status]] IN ('pending', 'failed') OR ([[status]] = 'running' AND [[leased_until]] < now()))
			  AND [[run_at]] <= now() + INTERVAL '1 second'
			ORDER BY [[run_at]] ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING [[id]], [[queue]], [[task]], [[payload]], [[status]], [[attempts]], [[max_retries]],
		          [[backoff_seconds]], [[idempotency_key]], [[last_error]], [[result]], [[run_at]],
		          [[leased_at]], [[leased_until]], [[leased_by]], [[created]], [[updated]]
	`).Bind(dbx.Params{
		"queue":    queue,
		"workerId": workerId,
		"leaseSec": leaseSec,
	}).One(job)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // No jobs available
		}
		return nil, fmt.Errorf("failed to claim next job: %w", err)
	}

	return job, nil
}

// Complete marks a job as successfully completed and stores the optional result.
func (jm *JobsManager) Complete(jobId string, result map[string]any) error {
	resultBytes, _ := json.Marshal(result)

	res, err := jm.app.DB().NewQuery(`
		UPDATE {{_jobs}}
		SET [[status]] = {:status},
		    [[result]] = {:result}::jsonb,
		    [[leased_by]] = '',
		    [[leased_until]] = NULL,
		    [[updated]] = now()
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{
		"id":     jobId,
		"status": JobStatusCompleted,
		"result": string(resultBytes),
	}).Execute()

	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("job %s not found", jobId)
	}
	return nil
}

// Fail transitions a job to failed (with exponential backoff) or dead (DLQ) if max retries exceeded.
func (jm *JobsManager) Fail(jobId string, jobErr error) error {
	job, err := jm.GetJob(jobId)
	if err != nil {
		return err
	}

	errMsg := ""
	if jobErr != nil {
		errMsg = jobErr.Error()
	}

	if job.Attempts >= job.MaxRetries {
		// Exhausted retries -> Dead Letter Queue
		_, err = jm.app.DB().NewQuery(`
			UPDATE {{_jobs}}
			SET [[status]] = {:status},
			    [[last_error]] = {:err},
			    [[leased_by]] = '',
			    [[leased_until]] = NULL,
			    [[updated]] = now()
			WHERE [[id]] = {:id}
		`).Bind(dbx.Params{
			"id":     jobId,
			"status": JobStatusDead,
			"err":    errMsg,
		}).Execute()
		return err
	}

	// Exponential backoff: backoff_seconds * 2^(attempts - 1)
	backoffSec := job.BackoffSeconds * (1 << (job.Attempts - 1))
	if backoffSec > 3600 {
		backoffSec = 3600 // Cap at 1 hour
	}

	_, err = jm.app.DB().NewQuery(`
		UPDATE {{_jobs}}
		SET [[status]] = {:status},
		    [[last_error]] = {:err},
		    [[leased_by]] = '',
		    [[leased_until]] = NULL,
		    [[run_at]] = now() + ({:backoffSec} * INTERVAL '1 second'),
		    [[updated]] = now()
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{
		"id":         jobId,
		"status":     JobStatusFailed,
		"err":        errMsg,
		"backoffSec": backoffSec,
	}).Execute()

	return err
}

// ReclaimStaleLeases resets crashed worker jobs whose lease has expired.
func (jm *JobsManager) ReclaimStaleLeases() (int64, error) {
	res, err := jm.app.DB().NewQuery(`
		UPDATE {{_jobs}}
		SET [[status]] = 'pending',
		    [[leased_by]] = '',
		    [[leased_until]] = NULL,
		    [[updated]] = now()
		WHERE [[status]] = 'running'
		  AND [[leased_until]] < now()
	`).Execute()

	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ReplayDeadJob resets a dead job from DLQ back to pending status for controlled operator replay.
func (jm *JobsManager) ReplayDeadJob(jobId string) error {
	res, err := jm.app.DB().NewQuery(`
		UPDATE {{_jobs}}
		SET [[status]] = {:status},
		    [[attempts]] = 0,
		    [[last_error]] = '',
		    [[run_at]] = now(),
		    [[updated]] = now()
		WHERE [[id]] = {:id}
		  AND [[status]] = 'dead'
	`).Bind(dbx.Params{
		"id":     jobId,
		"status": JobStatusPending,
	}).Execute()

	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("dead job %s not found or not in dead status", jobId)
	}
	return nil
}

// GetJob returns a job by ID.
func (jm *JobsManager) GetJob(jobId string) (*Job, error) {
	job := &Job{}
	err := jm.app.DB().NewQuery(`
		SELECT [[id]], [[queue]], [[task]], [[payload]], [[status]], [[attempts]], [[max_retries]],
		       [[backoff_seconds]], [[idempotency_key]], [[last_error]], [[result]], [[run_at]],
		       [[leased_at]], [[leased_until]], [[leased_by]], [[created]], [[updated]]
		FROM {{_jobs}}
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": jobId}).One(job)

	if err != nil {
		return nil, err
	}
	return job, nil
}

// ListJobs returns jobs filtered by queue and status.
func (jm *JobsManager) ListJobs(queue string, status string, limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 50
	}
	q := jm.app.DB().Select(
		"id", "queue", "task", "payload", "status", "attempts", "max_retries",
		"backoff_seconds", "idempotency_key", "last_error", "result", "run_at",
		"leased_at", "leased_until", "leased_by", "created", "updated",
	).From("_jobs").Limit(int64(limit)).OrderBy("created DESC")

	if queue != "" {
		q.AndWhere(dbx.HashExp{"queue": queue})
	}
	if status != "" {
		q.AndWhere(dbx.HashExp{"status": status})
	}

	var jobs []*Job
	err := q.All(&jobs)
	return jobs, err
}

// ProcessNext claims and processes the next eligible job on the queue.
func (jm *JobsManager) ProcessNext(ctx context.Context, queue string, workerId string, leaseDuration time.Duration) (bool, error) {
	job, err := jm.ClaimNext(queue, workerId, leaseDuration)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil // No job to process
	}

	jm.mu.RLock()
	handler, ok := jm.handlers[job.Task]
	jm.mu.RUnlock()

	if !ok {
		_ = jm.Fail(job.Id, fmt.Errorf("no handler registered for task: %s", job.Task))
		return true, fmt.Errorf("unregistered task handler: %s", job.Task)
	}

	execErr := handler(ctx, job)
	if execErr != nil {
		_ = jm.Fail(job.Id, execErr)
		return true, execErr
	}

	_ = jm.Complete(job.Id, nil)
	return true, nil
}

// PublishOutbox writes an outbox event atomically within the active db transaction.
func (jm *JobsManager) PublishOutbox(event string, topic string, payload map[string]any) (*OutboxEvent, error) {
	outbox := &OutboxEvent{
		Id:       security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789"),
		Event:    event,
		Topic:    topic,
		Payload:  payload,
		Status:   OutboxStatusPending,
		Attempts: 0,
		Created:  types.NowDateTime(),
	}

	payloadBytes, _ := json.Marshal(outbox.Payload)

	_, err := jm.app.DB().NewQuery(`
		INSERT INTO {{_outbox_events}} (
			[[id]], [[event]], [[topic]], [[payload]], [[status]], [[attempts]], [[created]]
		) VALUES (
			{:id}, {:event}, {:topic}, {:payload}::jsonb, {:status}, {:attempts}, {:created}
		)
	`).Bind(dbx.Params{
		"id":       outbox.Id,
		"event":    outbox.Event,
		"topic":    outbox.Topic,
		"payload":  string(payloadBytes),
		"status":   outbox.Status,
		"attempts": outbox.Attempts,
		"created":  outbox.Created.String(),
	}).Execute()

	if err != nil {
		return nil, fmt.Errorf("failed to publish outbox event: %w", err)
	}

	return outbox, nil
}

// ErrOutboxLeaseLost indicates that an outbox event's claim lease expired or was reclaimed by another worker.
var ErrOutboxLeaseLost = errors.New("outbox lease lost: claim token no longer active")

// ProcessPendingOutbox fetches and dispatches pending outbox events using the default 60-second lease.
func (jm *JobsManager) ProcessPendingOutbox(limit int, publisher func(evt *OutboxEvent) error) (int, error) {
	return jm.ProcessPendingOutboxWithLease(limit, 60*time.Second, publisher)
}

// ProcessPendingOutboxWithLease fetches and dispatches pending outbox events using the provided publisher
// with an explicit lease duration and unique per-claim fencing token.
func (jm *JobsManager) ProcessPendingOutboxWithLease(limit int, leaseDuration time.Duration, publisher func(evt *OutboxEvent) error) (int, error) {
	return jm.ProcessPendingOutboxWithOptions(context.Background(), OutboxProcessOptions{
		Limit:         limit,
		LeaseDuration: leaseDuration,
	}, func(_ context.Context, evt *OutboxEvent) error {
		return publisher(evt)
	})
}

// ProcessPendingOutboxWithOptions fetches and dispatches pending outbox events with full lease lifecycle
// management:
//   - Atomic single-statement claim using UPDATE ... RETURNING with FOR UPDATE SKIP LOCKED
//   - Sub-second millisecond lease precision and unique per-claim fencing tokens (claim_token)
//   - Batch lease expiration protection: before publishing subsequent events in a claimed batch
//     (or whenever remaining lease headroom drops below half the lease duration), verifies and
//     refreshes the event's lease in PostgreSQL fenced by claim_token AND leased_until > now().
//     Events whose lease already expired are skipped instead of blindly published.
//   - Optional background lease heartbeat (RenewInterval > 0) that extends leased_until while
//     a long-running publisher executes, canceling the publisher context if ownership is lost.
//   - Strict DB error propagation: claim, renewal, completion, and retry UPDATE errors are never
//     silently converted into successful publication counts.
//
// Delivery Semantics: At-Least-Once Delivery (NOT exactly-once).
// If a worker's lease expires while an external HTTP/broker dispatch remains in flight, another
// worker may reclaim and re-deliver the event. Similarly, if an external dispatch succeeds and the
// subsequent PostgreSQL acknowledgment fails or experiences an unknown transaction outcome, the
// event may be re-delivered after lease expiry. Downstream consumers MUST be idempotent and
// deduplicate using the stable OutboxEvent.Id or an application idempotency key in Payload.
func (jm *JobsManager) ProcessPendingOutboxWithOptions(
	ctx context.Context,
	opts OutboxProcessOptions,
	publisher func(ctx context.Context, evt *OutboxEvent) error,
) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = 60 * time.Second
	}
	leaseMs := opts.LeaseDuration.Milliseconds()
	if leaseMs < 50 {
		leaseMs = 50
	}

	if hooks := jm.getOutboxFaultHooks(); hooks != nil && hooks.beforeClaim != nil {
		if err := hooks.beforeClaim(); err != nil {
			return 0, fmt.Errorf("failed to claim pending outbox events: %w", err)
		}
	}

	var events []*OutboxEvent
	// Atomically claim rows: transition status to 'processing', assign a fresh claim token (fencing token),
	// record leased_at and leased_until, and increment attempts in a single UPDATE ... RETURNING statement
	// with FOR UPDATE SKIP LOCKED.
	//
	// Claim Criteria:
	// 1. Status is 'pending' (newly published or retried after failure), OR
	// 2. Status is 'processing' AND leased_until < now() (genuinely expired lease from a crashed or stalled worker).
	//
	// Note: We deliberately DO NOT check created < now() - INTERVAL '60s' because an event created hours ago
	// must NOT be prematurely reclaimed while its active worker holds a valid lease.
	err := jm.app.DB().NewQuery(`
		UPDATE {{_outbox_events}}
		SET [[status]] = 'processing',
		    [[attempts]] = [[attempts]] + 1,
		    [[claim_token]] = gen_random_uuid()::text,
		    [[leased_at]] = now(),
		    [[leased_until]] = now() + ({:leaseMs} * INTERVAL '1 millisecond')
		WHERE [[id]] IN (
			SELECT [[id]]
			FROM {{_outbox_events}}
			WHERE [[status]] = 'pending'
			   OR ([[status]] = 'processing' AND [[leased_until]] < now())
			ORDER BY [[created]] ASC
			FOR UPDATE SKIP LOCKED
			LIMIT {:limit}
		)
		RETURNING [[id]], [[event]], [[topic]], [[payload]], [[status]], [[attempts]], [[last_error]], [[claim_token]], [[leased_at]], [[leased_until]], [[created]], [[published_at]]
	`).WithContext(ctx).Bind(dbx.Params{
		"limit":   opts.Limit,
		"leaseMs": leaseMs,
	}).All(&events)

	if err != nil {
		return 0, fmt.Errorf("failed to claim pending outbox events: %w", err)
	}

	if hooks := jm.getOutboxFaultHooks(); hooks != nil && hooks.afterClaim != nil {
		if hookErr := hooks.afterClaim(events); hookErr != nil {
			return 0, hookErr
		}
	}

	count := 0
	var errs []error

	for idx, evt := range events {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}

		// Batch lease expiration guard: for subsequent events in a batch, or whenever remaining
		// lease headroom is below half the lease duration, perform a fenced lease check & refresh
		// against PostgreSQL's clock before invoking the external publisher. Also extend any
		// remaining still-valid tail events in the same claimed batch so healthy sequential
		// progress does not starve later events in the batch.
		remaining := time.Until(evt.LeasedUntil.Time())
		if idx > 0 || remaining <= opts.LeaseDuration/2 {
			refreshedUntil, renewErr := jm.renewOutboxLease(ctx, evt, leaseMs)
			if renewErr != nil {
				if errors.Is(renewErr, ErrOutboxLeaseLost) {
					jm.app.Logger().Warn("skipping outbox event publish: lease expired before publish started", "id", evt.Id, "token", evt.ClaimToken)
					continue
				}
				jm.app.Logger().Error("failed to verify/renew outbox lease before publish", "id", evt.Id, "error", renewErr)
				errs = append(errs, fmt.Errorf("outbox lease renewal failed for %s: %w", evt.Id, renewErr))
				continue
			}
			evt.LeasedUntil = refreshedUntil

			// Keep remaining still-valid events in this claimed batch alive while this worker makes progress.
			for j := idx + 1; j < len(events); j++ {
				if time.Until(events[j].LeasedUntil.Time()) <= opts.LeaseDuration/2 {
					if tailUntil, tailErr := jm.renewOutboxLease(ctx, events[j], leaseMs); tailErr == nil {
						events[j].LeasedUntil = tailUntil
					}
				}
			}
		}

		pubCtx := ctx
		var cancelPub context.CancelFunc
		if opts.PublishTimeout > 0 {
			pubCtx, cancelPub = context.WithTimeout(ctx, opts.PublishTimeout)
		} else {
			pubCtx, cancelPub = context.WithCancel(ctx)
		}

		var hbWg sync.WaitGroup
		stopHb := make(chan struct{})
		if opts.RenewInterval > 0 {
			hbWg.Add(1)
			go func(e *OutboxEvent) {
				defer hbWg.Done()
				ticker := time.NewTicker(opts.RenewInterval)
				defer ticker.Stop()
				for {
					select {
					case <-stopHb:
						return
					case <-pubCtx.Done():
						return
					case <-ticker.C:
						if _, rErr := jm.renewOutboxLease(pubCtx, e, leaseMs); rErr != nil {
							if errors.Is(rErr, ErrOutboxLeaseLost) {
								jm.app.Logger().Warn("outbox heartbeat lost lease ownership; canceling publisher", "id", e.Id, "token", e.ClaimToken)
								cancelPub()
								return
							}
							jm.app.Logger().Warn("outbox heartbeat renewal error", "id", e.Id, "error", rErr)
						}
					}
				}
			}(evt)
		}

		pubErr := publisher(pubCtx, evt)
		close(stopHb)
		hbWg.Wait()
		cancelPub()

		if pubErr != nil {
			newStatus := "pending"
			if evt.Attempts >= 5 {
				newStatus = "failed"
			}
			if hooks := jm.getOutboxFaultHooks(); hooks != nil && hooks.beforeMarkFail != nil {
				if hookErr := hooks.beforeMarkFail(evt); hookErr != nil {
					errs = append(errs, fmt.Errorf("failed to record outbox failure status for %s: %w", evt.Id, hookErr))
					continue
				}
			}
			// Fenced update: only transition status if our claim_token matches.
			// If our lease expired and another worker reclaimed this event, claim_token will differ,
			// preventing this stale worker from corrupting newer attempts or resetting retry counters.
			res, updateErr := jm.app.DB().NewQuery(`
				UPDATE {{_outbox_events}}
				SET [[status]] = {:status},
				    [[last_error]] = {:err},
				    [[leased_until]] = NULL
				WHERE [[id]] = {:id}
				  AND [[claim_token]] = {:claimToken}
				  AND [[status]] = 'processing'
			`).WithContext(ctx).Bind(dbx.Params{
				"id":         evt.Id,
				"claimToken": evt.ClaimToken,
				"status":     newStatus,
				"err":        pubErr.Error(),
			}).Execute()
			if updateErr != nil {
				jm.app.Logger().Error("failed to record outbox failure status", "id", evt.Id, "error", updateErr)
				errs = append(errs, fmt.Errorf("failed to record outbox failure status for %s: %w", evt.Id, updateErr))
			} else {
				affected, _ := res.RowsAffected()
				if affected == 0 {
					jm.app.Logger().Warn("outbox failure status update dropped: claim token fencing detected stale worker", "id", evt.Id, "token", evt.ClaimToken)
				}
			}
		} else {
			if hooks := jm.getOutboxFaultHooks(); hooks != nil && hooks.beforeMarkPub != nil {
				if hookErr := hooks.beforeMarkPub(evt); hookErr != nil {
					errs = append(errs, fmt.Errorf("failed to record outbox published status for %s: %w", evt.Id, hookErr))
					continue
				}
			}
			// Fenced update: only mark 'published' if our claim_token matches and status is still 'processing'.
			// If our lease expired and another worker reclaimed this event, claim_token will differ,
			// preventing this stale worker from overwriting the newer worker's state.
			res, updateErr := jm.app.DB().NewQuery(`
				UPDATE {{_outbox_events}}
				SET [[status]] = 'published',
				    [[published_at]] = now(),
				    [[last_error]] = '',
				    [[leased_until]] = NULL
				WHERE [[id]] = {:id}
				  AND [[claim_token]] = {:claimToken}
				  AND [[status]] = 'processing'
			`).WithContext(ctx).Bind(dbx.Params{
				"id":         evt.Id,
				"claimToken": evt.ClaimToken,
			}).Execute()
			if updateErr != nil {
				jm.app.Logger().Error("failed to record outbox published status", "id", evt.Id, "error", updateErr)
				errs = append(errs, fmt.Errorf("failed to record outbox published status for %s: %w", evt.Id, updateErr))
			} else {
				affected, _ := res.RowsAffected()
				if hooks := jm.getOutboxFaultHooks(); hooks != nil && hooks.afterMarkPub != nil {
					if hookErr := hooks.afterMarkPub(evt, affected); hookErr != nil {
						errs = append(errs, fmt.Errorf("unknown outcome after outbox published status update for %s: %w", evt.Id, hookErr))
						continue
					}
				}
				if affected > 0 {
					count++
				} else {
					jm.app.Logger().Warn("outbox published status update dropped: claim token fencing detected stale worker", "id", evt.Id, "token", evt.ClaimToken)
				}
			}
		}
	}

	return count, errors.Join(errs...)
}

func (jm *JobsManager) renewOutboxLease(ctx context.Context, evt *OutboxEvent, leaseMs int64) (types.DateTime, error) {
	if hooks := jm.getOutboxFaultHooks(); hooks != nil && hooks.beforeRenew != nil {
		if err := hooks.beforeRenew(evt); err != nil {
			return types.DateTime{}, err
		}
	}

	var refreshed types.DateTime
	err := jm.app.DB().NewQuery(`
		UPDATE {{_outbox_events}}
		SET [[leased_until]] = now() + ({:leaseMs} * INTERVAL '1 millisecond')
		WHERE [[id]] = {:id}
		  AND [[claim_token]] = {:claimToken}
		  AND [[status]] = 'processing'
		  AND [[leased_until]] > now()
		RETURNING [[leased_until]]
	`).WithContext(ctx).Bind(dbx.Params{
		"id":         evt.Id,
		"claimToken": evt.ClaimToken,
		"leaseMs":    leaseMs,
	}).Row(&refreshed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.DateTime{}, ErrOutboxLeaseLost
		}
		return types.DateTime{}, err
	}
	return refreshed, nil
}

// RegisterWebhook creates or updates a webhook target.
func (jm *JobsManager) RegisterWebhook(name, url, secret string, events []string) (*Webhook, error) {
	wb := &Webhook{
		Id:      security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789"),
		Name:    name,
		URL:     url,
		Secret:  secret,
		Events:  events,
		Active:  true,
		Created: types.NowDateTime(),
		Updated: types.NowDateTime(),
	}

	eventsBytes, _ := json.Marshal(wb.Events)

	_, err := jm.app.DB().NewQuery(`
		INSERT INTO {{_webhooks}} ([[id]], [[name]], [[url]], [[secret]], [[events]], [[active]], [[created]], [[updated]])
		VALUES ({:id}, {:name}, {:url}, {:secret}, {:events}::jsonb, {:active}, {:created}, {:updated})
	`).Bind(dbx.Params{
		"id":      wb.Id,
		"name":    wb.Name,
		"url":     wb.URL,
		"secret":  wb.Secret,
		"events":  string(eventsBytes),
		"active":  wb.Active,
		"created": wb.Created.String(),
		"updated": wb.Updated.String(),
	}).Execute()

	if err != nil {
		return nil, err
	}
	return wb, nil
}

// DeliverWebhook delivers an outbox event to a webhook endpoint with HMAC-SHA256 signature.
func (jm *JobsManager) DeliverWebhook(ctx context.Context, webhook *Webhook, event *OutboxEvent) (*WebhookAttempt, error) {
	start := time.Now()
	attempt := &WebhookAttempt{
		Id:        security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789"),
		WebhookId: webhook.Id,
		EventId:   event.Id,
		Created:   types.NowDateTime(),
	}

	payloadBytes, _ := json.Marshal(map[string]any{
		"id":        event.Id,
		"event":     event.Event,
		"topic":     event.Topic,
		"payload":   event.Payload,
		"timestamp": event.Created.String(),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, strings.NewReader(string(payloadBytes)))
	if err != nil {
		attempt.Error = err.Error()
		attempt.DurationMs = int(time.Since(start).Milliseconds())
		jm.recordWebhookAttempt(attempt)
		return attempt, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Ruxbase-Webhook/1.0")

	// Sign payload if secret is configured
	if webhook.Secret != "" {
		mac := hmac.New(sha256.New, []byte(webhook.Secret))
		mac.Write(payloadBytes)
		signature := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Ruxbase-Signature", signature)
	}

	resp, err := jm.httpClient.Do(req)
	attempt.DurationMs = int(time.Since(start).Milliseconds())
	if err != nil {
		attempt.Error = err.Error()
		jm.recordWebhookAttempt(attempt)
		return attempt, err
	}
	defer resp.Body.Close()

	attempt.StatusCode = resp.StatusCode
	bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	attempt.ResponseBody = string(bodySnippet)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		attempt.Error = fmt.Sprintf("webhook returned non-2xx status: %d", resp.StatusCode)
		jm.recordWebhookAttempt(attempt)
		return attempt, errors.New(attempt.Error)
	}

	jm.recordWebhookAttempt(attempt)
	return attempt, nil
}

func (jm *JobsManager) recordWebhookAttempt(attempt *WebhookAttempt) {
	_, _ = jm.app.DB().NewQuery(`
		INSERT INTO {{_webhook_attempts}} ([[id]], [[webhook_id]], [[event_id]], [[status_code]], [[response_body]], [[error]], [[duration_ms]], [[created]])
		VALUES ({:id}, {:webhookId}, {:eventId}, {:statusCode}, {:responseBody}, {:error}, {:durationMs}, {:created})
	`).Bind(dbx.Params{
		"id":           attempt.Id,
		"webhookId":    attempt.WebhookId,
		"eventId":      attempt.EventId,
		"statusCode":   attempt.StatusCode,
		"responseBody": attempt.ResponseBody,
		"error":        attempt.Error,
		"durationMs":   attempt.DurationMs,
		"created":      attempt.Created.String(),
	}).Execute()
}
