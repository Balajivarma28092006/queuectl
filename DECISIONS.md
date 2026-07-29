1. **Which exact line(s) prevent two workers from claiming the same job, and why is that operation atomic across separate OS processes?**

   The claim happens in `db.ClaimNextJob` (`db/jobs.go`):

   ```go
   row := database.QueryRow(`
       UPDATE jobs
       SET state = 'processing', worker_id = ?, lease_expires_at = ?, updated_at = ?
       WHERE id = (
           SELECT id FROM jobs
           WHERE state = 'pending'
               OR (state = 'failed' AND (next_run_at IS NULL OR next_run_at <= ?))
           ORDER BY created_at ASC
           LIMIT 1
       )
       RETURNING
   `+jobColumns, workerID, leaseMs, nowMs, nowMs)
   ```

   The important thing is that this is **one SQL statement**, not a "SELECT the id, then UPDATE that id" pair of round trips. The `id = (SELECT ... LIMIT 1)` subquery is evaluated *inside* the same `UPDATE`, and `RETURNING` hands back the row that was just written, all in a single call to `database.QueryRow`. There is no gap between "read the winning id" and "write processing/worker_id" where a second process could sneak in and read the same id — the read and the write are the same operation as far as SQLite is concerned.

   Why that's atomic *across processes* and not just within one Go process: SQLite serializes writers at the database-file level. `db.OpenAt` opens the file with `_journal_mode=WAL` and `_busy_timeout=5000` (`db/db.go`). WAL mode lets readers proceed concurrently with a writer, but it still only allows **one writer to hold the write lock at a time** — that lock is acquired via the OS's file-locking primitives on the `-wal`/`-shm` files, which is exactly what makes it safe across separate OS processes (each worker process, and even separate `queuectl worker start` invocations, are all talking to the same on-disk file through the same locking protocol). If worker B's UPDATE statement arrives while worker A's is mid-flight, SQLite blocks worker B until A's write transaction (the implicit one wrapping this single statement) commits, then B's subquery re-evaluates the `WHERE` clause against the *now-updated* table — so B's subquery simply won't see the row A just flipped to `processing` (since it no longer matches `state = 'pending'`/eligible `failed`). `_busy_timeout=5000` is what turns "blocked" into "wait up to 5s and retry" instead of an immediate `SQLITE_BUSY` error, so ordinary contention between workers doesn't surface as a hard failure.

   In short: the atomicity is guaranteed by (a) folding the "find the next job" and "claim it" into one statement so there's no read-then-write race window, and (b) SQLite's single-writer file lock, which is enforced by the OS across processes, not just goroutines.

2. **A worker is SIGKILL'd halfway through a job. Walk through, step by step, what state the job is in and how it eventually runs again. What is the worst-case delay before recovery?**

   SIGKILL cannot be caught, so none of the code in `handleWorkerStart`'s signal handler or `workerLoop`'s graceful-shutdown path runs. The process simply vanishes mid-instruction. Step by step:

   1. At the moment of the kill, the job row is `state='processing'`, `worker_id='<pid>-<n>'`, with a `lease_expires_at` that was last set either at claim time (`ClaimNextJob`) or at the most recent successful `RenewLease` heartbeat tick (every `leaseSeconds/3` inside `runJobWithLeaseRenewal`'s ticker goroutine).
   2. The heartbeat goroutine dies with the process, so no further `RenewLease` calls happen. The `lease_expires_at` value already written to the DB is now a hard deadline that will not be pushed forward again.
   3. The actual OS child process running the job's shell command (spawned via `exec.CommandContext` in `execJobCommand`) is **not** guaranteed to die with the parent — it isn't started in a way that ties its lifetime to a cancellable context that fires on SIGKILL (SIGKILL bypasses `ctx.Done()` entirely), so it can be reparented and keep running to completion, orphaned, with no worker watching it or able to record its result. This is a real gap in the design: the *row* gets recovered below, but the *actual command* may or may not still be executing somewhere.
   4. The row itself just sits in `processing` with a stale `lease_expires_at`. Nothing reaps it until some *other* worker loop iteration calls `db.ReapExpiredLeases`, which every worker calls at the top of every `workerLoop` iteration — but only when that worker is between jobs (finishing a job, or idle-polling every `db.PollInterval` = 2s). If every other worker happens to be busy running its own job, the check isn't attempted again until one of them frees up.
   5. Once some worker does call `ReapExpiredLeases` after `lease_expires_at` has passed, the UPDATE in that function fires: it increments `attempts`, sets `state='dead'` if `attempts+1 >= max_retries`, otherwise `state='pending'`, and clears `worker_id`/`lease_expires_at`.
   6. If it went back to `pending`, the next `ClaimNextJob` call (from any worker, ordered `created_at ASC` alongside whatever else is already pending) will pick it up in its normal turn and run it again from scratch — the command re-executes from the beginning, since queuectl has no notion of resuming partial work.

   Worst-case delay before recovery, using the default `lease_seconds=15` (and `poll_interval=2s`):
   - Up to `leaseSeconds` (≈15s) can elapse between the kill and `lease_expires_at` actually passing, if the kill happens right after a heartbeat renewal.
   - After that, recovery is *not* guaranteed on a fixed clock: `ReapExpiredLeases` only runs when some worker's loop reaches the top of its iteration. If a single worker process is running and it was the one killed, and no other worker process exists, nothing reaps the lease until a new `queuectl worker start` is run. Even with multiple workers, if they're all saturated running other (possibly long) jobs, the reap doesn't happen until one of them finishes its current job — so this portion is effectively unbounded in the presence of long-running jobs and no idle worker.
   - Assuming at least one worker is idle-polling, add up to `PollInterval` (2s) for that worker to notice on its next loop tick, plus whatever queue position the job lands in relative to other already-pending/eligible jobs (FIFO by `created_at`), plus another `PollInterval` window for the eventual claim.

   So the *typical* worst case (with idle capacity available) is roughly `lease_seconds + poll_interval` (~17s with defaults); the *true* worst case, if all workers stay continuously busy on other jobs, has no upper bound baked into the code — recovery is opportunistic, tied to some worker eventually going through its loop.

3. **Does dlq retry reset attempts? Why is that the right call?**

   Yes. `RetryDeadJobs` (`db/jobs.go`) does:

   ```go
   UPDATE jobs
   SET state = 'pending', attempts = 0, next_run_at = NULL,
   worker_id = NULL, updated_at = ?
   WHERE id = ? AND state = 'dead'
   ```

   as the comment above it states: *"Retry Dead jobs re-enqueues a DLQ job with a reset retry budget."*

   This is the right call because a job only ever reaches `dead` in the first place once `attempts >= max_retries` (see `MarkJobFailure`/`ReapExpiredLeases`) — by definition it has already exhausted its entire retry budget. If `dlq retry` left `attempts` untouched, the very next failure would immediately push `attempts` back to (or past) `max_retries` and `MarkJobFailure` would send it straight back to `dead` again after a single try, making the `retry` command nearly useless — an operator could never actually give the job the "the same number of tries you'd get for a fresh job" experience they're presumably asking for.

   Resetting `attempts` to `0` treats a manual DLQ retry as a deliberate operator decision — "whatever caused this to die is fixed now (bad config, downstream outage, bug patched), give it a full fresh shot" — rather than a mechanical continuation of the same failed attempt sequence. Clearing `next_run_at` and `worker_id` at the same time is equally important: it removes any lingering backoff timer from the old failure history and makes the job immediately eligible for `ClaimNextJob` instead of silently sitting until some future backoff deadline computed from stale state.

4. **What designs did you consider and reject for worker stop (cross-process signaling), and why?**

   The shipped design: `worker start` writes a PID file per worker process into `.queuectl/workers/<pid>.pid` and installs a `signal.Notify` handler for `SIGTERM`/`SIGINT` that cancels a `context.Context`, which stops the loop from claiming *new* jobs (but deliberately does **not** cancel the context passed into the currently-running job — `execCtx` in `runJobWithLeaseRenewal` is derived from `context.Background()`, not from the outer shutdown context — so an in-flight command is allowed to finish, matching the printed message *"finishing happening job or jobs then exiting..."*). `worker stop` reads every `*.pid` file and sends each process a `SIGTERM`.

   Alternatives considered and rejected:

   - **A "stop requested" flag written to the SQLite `config` table, polled by workers each loop iteration.** This reuses machinery that already exists (`GetConfigInt`/`SetConfig`), so it's tempting. Rejected because it adds a DB round trip to every loop iteration purely for control-plane signaling, it can only be noticed at the next poll (up to `PollInterval` = 2s of extra latency, whereas a signal interrupts the process's blocking select immediately), it doesn't target a *specific* worker process (every worker reads the same global flag, so you can't stop one of several `worker start --count N` processes independently the way per-PID signaling does), and it still needs someone to clear the flag afterward or new workers would refuse to start.
   - **A Unix domain socket / named pipe that `worker stop` connects to and writes a "stop" message on.** Gives cleaner point-to-point semantics than a shared file, but requires a listener goroutine, socket-path management, cleanup on crash, and behaves differently enough on Windows to complicate the cross-platform story that `execJobCommand` already has to special-case (`cmd /C` vs `sh -c`). Signals are a simpler, already-cross-platform-supported (Go's `os/signal` covers Windows too, if with a smaller signal set) primitive for exactly this "tell a known PID to shut down" use case.
   - **Killing the job's child process (and its process group) on stop, not just stopping new claims.** Rejected because "stop" is meant to be graceful — the job that's currently running represents work already claimed with a lease; forcibly killing it would abandon it mid-command with the same orphaned-process risk described in Q2, and would require someone to eventually reap its lease anyway. Letting it finish naturally and simply refusing to pick up the *next* job is strictly less disruptive and reuses the existing lease/reap machinery as a safety net rather than fighting it.
   - **Relying on the user sending SIGKILL directly (no `worker stop` command at all).** Rejected for the obvious reason that it's exactly the ungraceful path analyzed in Q2 — orphaned child processes, no chance to let an in-flight job finish, and recovery entirely dependent on lease expiry/reaping rather than an immediate, deliberate handoff.

   PID files plus `SIGTERM` won out because they need nothing more than the filesystem (already used for other bookkeeping) to discover which OS processes are workers, they use the OS's own well-understood signal-delivery semantics instead of inventing a bespoke IPC channel, they let each worker process decide for itself when it's safe to stop (finish current job, stop claiming new ones) rather than being killed out from under a job, and `worker stop` naturally generalizes to stopping several worker processes (`--count`-started or separately invoked) with one command by iterating every `*.pid` file it finds.

5. **If priorities were added tomorrow (high-priority jobs jump the queue), which parts of your design survive unchanged and which break?**

   **Survives unchanged:**
   - The atomic single-statement claim pattern in `ClaimNextJob` (Q1) is unaffected in principle — you'd only change the subquery's `ORDER BY created_at ASC` to something like `ORDER BY priority DESC, created_at ASC`. The "fold the read and the write into one UPDATE...RETURNING" trick, and the reliance on SQLite's single-writer lock for cross-process safety, don't need to change at all.
   - Everything that operates on an already-claimed row by `(id, worker_id, state)` — `RenewLease`, `MarkJobSuccess`, `MarkJobFailure`, `ReapExpiredLeases` — is completely orthogonal to priority. None of these queries reference ordering; they only care about *which specific row* a given worker owns and its current state. The lease/heartbeat/crash-recovery story from Q2 is untouched.
   - `RetryDeadJobs` and the DLQ flow (Q3) are unaffected; a job's priority is just another column carried along for the ride, unrelated to why it died or how its retry budget resets.
   - The `worker stop`/`SIGTERM`/PID-file mechanism (Q4) is entirely orthogonal to how jobs get selected, so it survives untouched.
   - The `config` table pattern (`GetConfigInt`/`GetConfigFloat`/`SetConfig`) generalizes cleanly to any new priority-related tunable (e.g., an aging factor) without schema changes to that table.

   **Breaks or needs real work:**
   - `enqueue.go` explicitly documents and relies on a no-priority guarantee: the comment *"no priority only first come basis"* becomes false, and `validateInputs` would need to gain range/type validation for a new `priority` field (plus a documented default for jobs that don't specify one).
   - The `ORDER BY created_at ASC` change itself is simple, but a **naive** "always run the highest priority pending job next" policy can starve low-priority jobs indefinitely if a steady stream of high-priority work keeps arriving — nothing in the current design has any notion of aging/fairness, and bolting that on (e.g., an age-boosted effective priority) is a nontrivial addition, not a one-line fix.
   - The interaction between priority and the existing `state='pending' OR (state='failed' AND next_run_at <= now)` claim condition needs a real decision: should a high-priority job that's back from backoff jump ahead of a low-priority job that's merely pending? Almost certainly yes, which is easy to express in SQL (`ORDER BY priority DESC, created_at ASC` covers both branches) but is a semantic change worth calling out explicitly, not an implicit consequence.
   - Performance: there's currently no index beyond the `jobs.id` primary key, so `ClaimNextJob`'s subquery is already a full-table scan under contention; sorting by priority as well makes an index (e.g., on `(state, priority, created_at)`) considerably more worth adding than it was before, whereas today the table is small enough not to notice.
  
