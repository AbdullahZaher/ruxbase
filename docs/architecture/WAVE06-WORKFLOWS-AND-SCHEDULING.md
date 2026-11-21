# Wave 06: Workflows, Branching & Scheduling Architecture

## 1. Architectural Scope & Purpose

Wave 06 Milestone **W06-06** specifies user-facing durable workflows, conditional step branching, and future/recurring scheduling built natively on PostgreSQL without introducing mandatory external orchestrators (e.g., Temporal, Airflow, or Kafka).

While the core durable job engine (`_jobs`) handles atomic, single-task background execution with leased workers, workflows provide multi-step business orchestration where intermediate state, outputs, conditional branching, and error handling are persisted transactionally in PostgreSQL.

---

## 2. PostgreSQL-Native Workflow Model

### 2.1 State & Persistence Boundary

Workflows reside in the `_workflows` table:

```sql
CREATE TABLE IF NOT EXISTS _workflows (
    id            TEXT PRIMARY KEY NOT NULL,
    name          TEXT NOT NULL,
    status        TEXT DEFAULT 'pending' NOT NULL, -- pending, running, completed, failed
    current_step  INTEGER DEFAULT 0 NOT NULL,
    steps         JSONB NOT NULL,                  -- array of WorkflowStep definitions
    context       JSONB NOT NULL,                  -- execution context dictionary passed across steps
    schedule_cron TEXT DEFAULT '' NOT NULL,        -- optional cron schedule
    next_run      TIMESTAMPTZ,                     -- scheduled execution timestamp
    last_error    TEXT DEFAULT '' NOT NULL,
    created       TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated       TIMESTAMPTZ DEFAULT now() NOT NULL
);
CREATE INDEX IF NOT EXISTS idx__workflows_status_next_run ON _workflows (status, next_run);
```

### 2.2 Workflow Step Lifecycle

Each step in `steps` defines:
- `name`: Unique step identifier within the workflow.
- `task`: Registered job task to execute.
- `condition`: Optional boolean context evaluation key (e.g. `requires_approval == true`).
- `on_success`: Target step name upon success (empty proceeds to step `index + 1`).
- `on_failure`: Target step name upon failure (empty halts workflow as `failed`).

### 2.3 Execution Flow & Branching Semantics

1. **Step Execution**: The engine loads the workflow with row-level lock (`FOR UPDATE SKIP LOCKED`), resolves the current step, merges inputs from the workflow `context`, and dispatches the task handler.
2. **Context Propagation**: The handler's output map is merged into `workflow.context`.
3. **Branching**:
   - If the step succeeds, next step is determined by `on_success` or incrementing `current_step`.
   - If a step condition evaluates to false, the step is skipped and execution advances.
   - If the step fails, `on_failure` step is invoked if configured; otherwise, workflow transitions to `failed` and records `last_error`.
4. **Completion**: When no further steps remain, status transitions to `completed`.

---

## 3. Scheduling & Deferred Execution

Workflows support both one-shot future execution (`next_run <= now()`) and recurring schedules via standard cron strings:
- Workflows with `status = 'pending'` and `next_run <= now()` are claimed atomically by background dispatchers.
- After completion of a scheduled recurring workflow, `next_run` is recalculated and status resets to `pending`.

---

## 4. Crash Recovery & Exactly-Once Idempotency

- Every step state transition is committed to PostgreSQL in the active transaction before subsequent step execution begins.
- If a node crashes mid-step, uncommitted step updates roll back in PostgreSQL, allowing survivor nodes to replay from the last committed step checkpoint.
