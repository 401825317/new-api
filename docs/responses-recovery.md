# Responses Recovery Lab

This is an opt-in patch over official New API, not a replacement fork.
Base: `v0.13.1-patch.1`. Production is not modified by this branch.

## Scope

- Enable with `RESPONSES_STREAM_RECOVERY_ENABLED=true` on an isolated service.
- Only native streaming `POST /v1/responses` is in scope. Other endpoints and non-streaming requests retain official behavior.
- Buffer structural `response.created` / `response.in_progress` / empty tool metadata preambles (256 KiB maximum). Do not commit or send pre-output metadata as semantic output.
- Recognize `error`, `response.error`, `response.failed`, incomplete/cancelled responses, malformed events and missing successful terminal events.
- Before downstream commitment: return transient failures to the recovery retry loop. 429, 500-503 and other transient 5xx may switch channel; 504/524 are excluded by default because the upstream may already have executed the request. Never retry the same channel within a request. Select the highest remaining eligible priority, weighted within that tier.
- After commitment or reported usage: never replay. Forward the failure once, or synthesize a Responses error for a truncated stream. Record failure separately from usage settlement.
- Invalid input, normal incomplete responses, client cancellation and requests using `previous_response_id` do not replay. All retries still obey configured status-code rules, affinity skip-retry and specific-channel constraints.
- Successful terminal events must have `response.status=completed`; stop immediately instead of waiting for TCP EOF.
- Penalize a transient failure for its group/model/channel only, default 30 seconds (`RESPONSES_RECOVERY_COOLDOWN_SECONDS`, range 1-300). No account/channel is forcibly enabled.
- Cooldown suppresses stale affinity temporarily. Failed requests do not refresh affinity; successful fallback binds its actual channel. No unsafe GET-then-DELETE of a concurrent successful binding.
- Optional dynamic channel weighting can adjust only the effective weight inside one priority tier. It uses a process-local recent window of real Responses FRT and outcomes, requires a minimum sample count, and never writes over operator-configured `priority`/`weight`. It is disabled by default and is configured under the monitoring settings page.
- Pre-output body deadline defaults to 90 seconds (`RESPONSES_RECOVERY_PREOUTPUT_SECONDS`, range 1-300). It starts after response headers. Configure the normal relay HTTP timeout as well to bound connection/header waits.
- Redis mode shares cooldown between instances. Without Redis, cooldown is process-local and bounded to 10,000 routes. Do not use production Redis for the lab.

## Billing And Safety Limits

Pre-output failed attempts do not settle usage. The eventual successful attempt settles once. If all attempts fail, the existing billing session refunds its reservation. Partial failures retain reported usage; absent usage is not guessed for a failed stream. Successful streams without usage retain official text-token estimation.

This cannot create upstream capacity or guarantee exactly-once execution at the provider: a provider can perform work before any event arrives. Unknown events and tool events conservatively end the replay window. Stateful response IDs are not portable between independent upstreams.

The 50-concurrent-request test is deterministic fault injection against local HTTP upstreams, not a claim that a real provider supports 50 or 1,000 concurrent generations.

## Verification

```powershell
go test ./controller ./relay/channel/openai ./service ./logger -run TestResponsesRecovery -count=3 -timeout 120s
go test ./model ./middleware ./relay/channel -count=1 -timeout 120s
```

The full-relay test exercises actual distribution, HTTP adapters, retry controller, error logs, wallet/token settlement and affinity with an isolated SQLite database. It runs both channel-cache modes, 50 simultaneous failed initial attempts followed by 50 successful fallbacks, partial output failure, exhaustion/refund and an opt-out official-behavior baseline. Parser tests cover EOF, malformed events, failure shapes, multiline SSE, cancellation, stateful requests, deadlines, open-stream termination and 100 independent concurrent streams.

Windows full-service tests can collide in upstream affinity-usage test keys generated with `UnixNano`; the untouched official `v0.13.2` also reproduces this failure. Do not confuse that baseline issue with passing recovery tests.

The lab Dockerfile requires Linux race tests to pass before producing an image. These tests exposed an existing unsynchronized logging counter and rotation flag in `logger/logger.go`; the patch uses atomic state for both, with a concurrent logging regression test. This is separate from the original SSE failure and is not claimed as the cause of provider overload.

The explicitly disabled-feature `OfficialBaseline` subtest reproduces a separate existing close/send race in the untouched shared scanner. It runs in the ordinary regression gate; only this old-path baseline is excluded from the race gate. All new recovery tests, including both cache modes, concurrent requests and billing, remain under race detection. The patch deliberately does not refactor the shared scanner used by other protocols.

## Upgrading

Keep official tags and this small patch as separate history. Commit the recovery branch, fetch the intended official release, then run:

```powershell
.\scripts\verify-responses-recovery.ps1 -UpstreamRef v0.13.2 -TargetDirectory C:\work\newapi-recovery-v0132
```

The script creates a separate detached worktree, checks patch applicability and runs regression tests. A conflict or test failure stops the upgrade. It does not deploy, change the current branch, touch databases or resolve conflicts automatically. Review upstream security/migrations and run the full suite on Linux before building a new immutable image.

## Isolation And Rollback

Use a separate service, database/volume, cache and dedicated test-only cf-global group/account/key. Never attach the existing production database, Redis, volumes or group 165. Start with mock channels, then a minimal explicitly scoped live request. Do not use a production API key for a concurrent test.

Rollback is switching off the environment flag and restarting only the lab, or restoring its previous pinned image. No database migration is introduced. Production deployment remains a separate decision.

`Dockerfile.recovery-lab` is explicitly for the v0.13.2 test image: it includes a loopback-only fault injector on port 3101. Never use that image for customer routing. The ordinary official `Dockerfile` is unchanged. For Zeabur local source upload, export only committed source, retain tracked `web/bun.lock` (the CLI otherwise drops it because of `.gitignore`), and select the lab Dockerfile in the exported build directory. Explicit `registry-1.docker.io` references preserve official image digests while avoiding a failing platform mirror.
