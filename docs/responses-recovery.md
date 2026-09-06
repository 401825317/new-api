# Responses Recovery Lab

This is an opt-in patch over official New API, not a replacement fork.
Base: `v0.13.1-patch.1`. Production is not modified by this branch.

## Scope

- Enable with `RESPONSES_STREAM_RECOVERY_ENABLED=true` on an isolated service.
- Only native streaming `POST /v1/responses` is in scope. Other endpoints and non-streaming requests retain official behavior.
- Buffer only empty `response.created` / `response.in_progress` preambles (64 KiB maximum). Do not send pre-output pings or SSE headers.
- Recognize `error`, `response.error`, `response.failed`, incomplete/cancelled responses, malformed events and missing successful terminal events.
- Before downstream commitment: return transient failures to the existing retry loop. Never retry the same channel within a request. Select the highest remaining eligible priority, weighted within that tier.
- After commitment or reported usage: never replay. Forward the failure once, or synthesize a Responses error for a truncated stream. Record failure separately from usage settlement.
- Invalid input, normal incomplete responses, client cancellation and requests using `previous_response_id` do not replay. All retries still obey configured status-code rules, affinity skip-retry and specific-channel constraints.
- Successful terminal events must have `response.status=completed`; stop immediately instead of waiting for TCP EOF.
- Penalize a transient failure for its group/model/channel only, default 30 seconds (`RESPONSES_RECOVERY_COOLDOWN_SECONDS`, range 1-300). No account/channel is forcibly enabled.
- Cooldown suppresses stale affinity temporarily. Failed requests do not refresh affinity; successful fallback binds its actual channel. No unsafe GET-then-DELETE of a concurrent successful binding.
- Pre-output body deadline defaults to 90 seconds (`RESPONSES_RECOVERY_PREOUTPUT_SECONDS`, range 1-300). It starts after response headers. Configure the normal relay HTTP timeout as well to bound connection/header waits.
- Redis mode shares cooldown between instances. Without Redis, cooldown is process-local and bounded to 10,000 routes. Do not use production Redis for the lab.

## Billing And Safety Limits

Pre-output failed attempts do not settle usage. The eventual successful attempt settles once. If all attempts fail, the existing billing session refunds its reservation. Partial failures retain reported usage; absent usage is not guessed for a failed stream. Successful streams without usage retain official text-token estimation.

This cannot create upstream capacity or guarantee exactly-once execution at the provider: a provider can perform work before any event arrives. Unknown events and tool events conservatively end the replay window. Stateful response IDs are not portable between independent upstreams.

The 50-concurrent-request test is deterministic fault injection against local HTTP upstreams, not a claim that a real provider supports 50 or 1,000 concurrent generations.

## Verification

```powershell
go test ./controller ./relay/channel/openai ./service -run TestResponsesRecovery -count=3 -timeout 120s
go test ./model ./middleware ./relay/channel -count=1 -timeout 120s
```

The full-relay test exercises actual distribution, HTTP adapters, retry controller, error logs, wallet/token settlement and affinity with an isolated SQLite database. It runs both channel-cache modes, 50 simultaneous failed initial attempts followed by 50 successful fallbacks, partial output failure, exhaustion/refund and an opt-out official-behavior baseline. Parser tests cover EOF, malformed events, failure shapes, multiline SSE, cancellation, stateful requests, deadlines, open-stream termination and 100 independent concurrent streams.

Windows full-service tests can collide in upstream affinity-usage test keys generated with `UnixNano`; the untouched official `v0.13.2` also reproduces this failure. Do not confuse that baseline issue with passing recovery tests. Run race detection on a host with CGO and a C compiler before promoting to production.

## Upgrading

Keep official tags and this small patch as separate history. Commit the recovery branch, fetch the intended official release, then run:

```powershell
.\scripts\verify-responses-recovery.ps1 -UpstreamRef v0.13.2 -TargetDirectory C:\work\newapi-recovery-v0132
```

The script creates a separate detached worktree, checks patch applicability and runs regression tests. A conflict or test failure stops the upgrade. It does not deploy, change the current branch, touch databases or resolve conflicts automatically. Review upstream security/migrations and run the full suite on Linux before building a new immutable image.

## Isolation And Rollback

Use a separate service, database/volume, cache and dedicated test-only cf-global group/account/key. Never attach the existing production database, Redis, volumes or group 165. Start with mock channels, then a minimal explicitly scoped live request. Do not use a production API key for a concurrent test.

Rollback is switching off the environment flag and restarting only the lab, or restoring its previous pinned image. No database migration is introduced. Production deployment remains a separate decision.
