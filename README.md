# queuectl

`queuectl` is a small, single-binary, SQLite-backed job queue and worker pool
you drive entirely from the command line. Jobs are shell commands; workers
are plain OS processes that poll a shared SQLite database for work. There's
no server to run and no external broker — the database file *is* the queue,
and any process on the machine (or a shared filesystem) that opens the same
`queue.db` participates in the same queue.

It gives you:

- **At-least-once execution** with lease-based ownership, so a crashed
  worker's job gets picked up again automatically.
- **Automatic retries with exponential backoff**, and a dead-letter queue
  (DLQ) for jobs that exhaust their retry budget.
- **Graceful worker shutdown** across separate processes via PID files and
  `SIGTERM`, so `worker stop` run from another terminal finishes in-flight
  jobs before exiting.
- **Runtime-tunable config** (max retries, backoff base, lease duration)
  stored in the same database, no restart required.

## Requirements

- Go 1.26+ (see `go.mod`)
- A C toolchain (`gcc`/`cc`) available on `PATH` — `github.com/mattn/go-sqlite3`
  is a cgo binding to SQLite, so `CGO_ENABLED=1` (the default) is required to
  build.

## Install / Build

```bash
git clone https://github.com/BalajiVarma28092006/queuectl.git
cd queuectl
go build -o queuectl .
```

This produces a `queuectl` binary in the current directory. Optionally,
install it onto your `$PATH`:

```bash
go install github.com/BalajiVarma28092006/queuectl@latest
```

Running any `queuectl` command from a given directory creates (or reuses)
a `queue.db` SQLite file and a `.queuectl/workers/` directory in that
**current working directory** — so run all `queuectl` commands (enqueue,
worker start, status, etc.) from the same directory to operate on the same
queue.

## Quick start

```bash
# 1. Enqueue a job. `id` and `command` are required.
./queuectl enqueue '{"id":"job-1","command":"echo hello world"}'

# 2. Start a worker pool (foreground process; Ctrl+C to stop).
./queuectl worker start --count 3

# 3. From another terminal, check on things.
./queuectl status
./queuectl list --state completed

# 4. Ask a running worker pool to shut down gracefully.
./queuectl worker stop
```

## Commands

### `enqueue`

```bash
queuectl enqueue '<json_payload>'
```

Adds a job. The JSON payload is unmarshaled directly into the internal job
struct, so any of its fields can be set, but in practice you only need:

| field         | required | notes                                                              |
|---------------|----------|---------------------------------------------------------------------|
| `id`          | yes      | must be unique; re-using an existing id is rejected                |
| `command`     | yes      | a shell command string, run via `sh -c` (or `cmd /C` on Windows)   |
| `max_retries` | no       | defaults to the current `max_retries` config value if omitted/`0` |

New jobs always start in `pending` state with `attempts=0`. There is no
priority — jobs are served strictly first-come, first-served, ordered by
enqueue time.

Example:

```bash
queuectl enqueue '{"id":"nightly-backup","command":"tar czf /backups/$(date +%F).tgz /data","max_retries":5}'
```

### `worker start [--count N]`

```bash
queuectl worker start --count 3
```

Starts `N` (default `1`) worker goroutines inside a single OS process. Each
goroutine repeatedly:

1. Reaps any jobs whose lease has silently expired (crash recovery — see
   below).
2. Atomically claims the oldest eligible job (`pending`, or `failed` whose
   backoff has elapsed).
3. Runs the job's command as a subprocess, renewing its lease periodically
   while the command runs.
4. Records success or failure, applying exponential backoff or moving the
   job to the DLQ if retries are exhausted.
5. If no job is available, sleeps for a short poll interval and tries again.

The process writes a PID file to `.queuectl/workers/<pid>.pid` for the
duration of its run, and listens for `SIGINT`/`SIGTERM` to shut down
gracefully: it stops claiming new jobs but lets any job currently in flight
finish before exiting. You can run multiple `worker start` invocations (in
different terminals, or as separate background processes) against the same
`queue.db` to add capacity; they coordinate purely through the database.

### `worker stop`

```bash
queuectl worker stop
```

Looks at every PID file under `.queuectl/workers/` and sends `SIGTERM` to
each live process, asking it to shut down gracefully (finish current job,
then exit) — equivalent to pressing Ctrl+C in each worker's terminal, but
from anywhere.

### `status`

```bash
queuectl status
```

Prints a count of jobs in each state (`pending`, `processing`, `failed`,
`completed`, `dead`), the PIDs of currently live worker processes, and the
effective `max_retries`/`backoff_base` config values.

### `list`

```bash
queuectl list --state <state> [--json]
```

Lists jobs in a given state (`pending`, `processing`, `failed`, `completed`,
or `dead`). `--state` is required. Add `--json` for a machine-readable
array of full job records instead of the human-readable summary.

```bash
queuectl list --state failed --json
```

### `dlq`

```bash
queuectl dlq list             # show all dead jobs
queuectl dlq retry <id>       # re-enqueue one dead job with a fresh retry budget
queuectl dlq retry --all            # re-enqueue every dead job
```

Jobs land in the DLQ (`state=dead`) once they've failed `max_retries` times.
`dlq retry` resets `attempts` back to `0` and clears any pending backoff
timer, so the job gets a full, fresh set of retry attempts rather than
immediately dying again on its next failure — see `DECISIONS.md` (Q3) for
the reasoning.

### `config`

```bash
queuectl config show
queuectl config set max_retries <n>          # default retry budget for new jobs
queuectl config set backoff-base <f>         # exponential backoff base, must be > 1
queuectl config set lease-seconds <n>        # how long a worker "owns" a job before it's reclaimable
```

Config is stored in the database itself (a `config` key/value table), so
changes apply immediately to any worker process reading it — no restart
needed. `lease-seconds` is capped automatically to keep total crash-recovery
time bounded (see `config set lease-seconds` output for the current max);
attempting to set it too high is rejected with an explanatory error.

## How reliability works (short version)

- **Claiming is atomic across processes.** A worker claims the next job with
  a single `UPDATE ... WHERE id = (SELECT ...) RETURNING` statement, so two
  workers (even in different OS processes) can never claim the same job —
  SQLite's single-writer lock on the database file serializes the
  statements. See `DECISIONS.md` (Q1) for the full explanation.
- **Crash recovery via leases.** When a worker claims a job it takes out a
  time-boxed lease (`lease_expires_at`) and renews it periodically while the
  job runs. If a worker dies (crash, `SIGKILL`, power loss) mid-job, it stops
  renewing the lease. Once the lease expires, any other worker's normal loop
  notices via `ReapExpiredLeases` and puts the job back to `pending` (or
  `dead`, if it's out of retries) so it runs again. See `DECISIONS.md` (Q2)
  for the exact timeline and worst-case delay.
- **Exponential backoff on failure.** A failed job (nonzero exit code from
  its command) is scheduled to retry after `backoff_base ^ attempts`
  seconds, up to `max_retries` attempts, before being moved to the DLQ.
- **Graceful, cross-process worker shutdown.** `worker stop` signals running
  worker processes by PID rather than requiring them to poll a flag, so
  shutdown is near-instant and doesn't add per-loop-iteration overhead. See
  `DECISIONS.md` (Q4).

For an in-depth, line-referenced discussion of these mechanisms — including
the tradeoffs considered and current limitations (e.g. a `SIGKILL`'d
worker's in-flight subprocess isn't guaranteed to be killed alongside it) —
see [`DECISIONS.md`](./DECISIONS.md).

## Development

Run the test suite:

```bash
go test ./...
```

Tests currently cover the `db` package's config helpers
(`db/config_test.go`) against an in-memory SQLite database; job-claiming
and lease-recovery behavior (`db/jobs_test.go`) is a good place to add
coverage next.

## Known limitations

- A job that fails partway through re-runs **from the beginning** on retry
  — `queuectl` has no notion of resumable/idempotent-checkpointed work, so
  commands should be written to be safe to re-run.
- If a worker process is `SIGKILL`'d, the shell subprocess it spawned is not
  guaranteed to be terminated and may keep running, orphaned, independent of
  the queue's bookkeeping (see `DECISIONS.md`, Q2).
- No job priority — strictly FIFO by enqueue time (see `DECISIONS.md`, Q5
  for what would need to change to add it).
- `queue.db` and `.queuectl/workers/` are resolved relative to the current
  working directory; there's no `--db` flag to point at a queue elsewhere.
