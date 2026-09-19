# Responses Recovery Lab

This is an opt-in patch over official New API, not a replacement fork.
Base: `v0.13.1-patch.1`. Production is not modified by this branch.

## Scope

### 2026-09-19 missing-state follow-up

The HTTP 400 thinking fallback now recognizes both `reasoning_text` and `reasoning_content`, including backtick-quoted field names and upstream error prefixes. Live channel 27 errors used the latter spelling, which the prior release did not recognize. The same-channel, same-credential, one-retry boundary and error-only envelope checks remain unchanged. Regression tests reproduce the previously missed variants and exclude context-limit errors and responses containing usage or output.

This correction does not fix context exhaustion: a request can first fail missing-state validation on channel 27 and then exceed the context limit on channel 38. Do not silently drop conversation/tool history or replay context-limit failures with unchanged input. Actual input size and the upstream treatment of replayed reasoning still require separate verification.

Deployment increment: official-B application image only; no new SQL, Redis administration, or environment changes. This follow-up requires a new build/deployment and live verification of channel 27 accepting the thinking-off retry before it can be reported as resolved. Roll back to the immediately preceding immutable image if verification fails.

- Enable with `RESPONSES_STREAM_RECOVERY_ENABLED=true` on an isolated service.
- Stream recovery only covers native streaming `POST /v1/responses`. Continuation routing also covers non-streaming Responses; other endpoints retain official behavior.
- Buffer structural `response.created` / `response.in_progress` / empty tool metadata preambles. Do not commit or send pre-output metadata as semantic output. The ceiling defaults to 256 KiB (`RESPONSES_RECOVERY_PREAMBLE_LIMIT_KB`, range 256-16384).
- Some upstreams echo the whole request (`tools`, `instructions`) inside both `response.created` and `response.in_progress`, so a legitimate preamble is roughly twice the request size. With the 256 KiB default, any request whose `tools`+`instructions` exceeds ~128 KiB fails with `responses_preoutput_buffer_exceeded`; raise the ceiling for those upstreams instead of disabling recovery. The buffer only ever holds a single request's preamble, so peak memory scales with concurrency times the configured ceiling.
- Recognize `error`, `response.error`, `response.failed`, incomplete/cancelled responses, malformed events and missing successful terminal events.
- Before downstream commitment: return transient failures to the recovery retry loop. 429, 500-503 and other transient 5xx may switch channel; 504/524 are excluded by default because the upstream may already have executed the request. Never retry the same channel within a request. Select the highest remaining eligible priority, weighted within that tier.
- After commitment or reported usage: never replay inside the gateway. Forward the failure once, or synthesize a Responses error for a truncated stream. Committed `error` events use the standard top-level `code` / `message` shape that OpenClaw parses; `response.failed` retains its event type and receives the same status-bearing error message. This lets UClaw classify 429/5xx/timeout/network failures and perform its side-effect-safe delayed replacement without appending a second answer inside the already committed HTTP stream. Record failure separately from usage settlement.
- Invalid input, normal incomplete responses, client cancellation and requests using `previous_response_id` do not replay. All retries still obey configured status-code rules, affinity skip-retry and specific-channel constraints.
- Successful terminal events must have `response.status=completed`; stop immediately instead of waiting for TCP EOF.
- Penalize a transient failure for its group/model/channel only, default 30 seconds (`RESPONSES_RECOVERY_COOLDOWN_SECONDS`, range 1-300). No account/channel is forcibly enabled.
- Cooldown suppresses stale affinity temporarily. Failed requests do not refresh affinity; successful fallback binds its actual channel. No unsafe GET-then-DELETE of a concurrent successful binding.
- Optional dynamic channel weighting can adjust only the effective weight inside one priority tier. It uses a process-local recent window of real Responses FRT and outcomes, requires a minimum sample count, and never writes over operator-configured `priority`/`weight`. It is disabled by default and is configured under the monitoring settings page.
- Pre-output body deadline defaults to 90 seconds (`RESPONSES_RECOVERY_PREOUTPUT_SECONDS`, range 1-300). It starts after response headers. Configure the normal relay HTTP timeout as well to bound connection/header waits.
- Redis mode shares cooldown between instances. Without Redis, cooldown is process-local and bounded to 10,000 routes. Do not use production Redis for the lab.

## Billing And Safety Limits

### Continuation routing (2026-09-19)

Successful completed Responses bind their response ID to the actual channel for one hour, before the terminal event is released. Bindings are scoped to user, selected group and original model; cache keys are hashed and no payload or API key is stored. Redis shares bindings when configured; otherwise the bounded 100,000-entry cache is process-local. This introduces cache writes on deployment, but no schema migration.

Follow-up requests carrying `previous_response_id` consult the binding before ordinary affinity or weighted selection. Auto-group requests search only the user's currently usable groups. Disabled channels and removed group/model abilities fail closed. Cooldown does not redirect a known stateful binding: its original enabled channel remains the only valid target. Stateful requests cannot enter the generic cross-channel retry loop, even when streaming recovery is disabled.

Multi-key channels are excluded because a channel ID alone cannot pin the credential. Configure separate single-key channels, and avoid rotating their credentials during active continuations. Unknown/expired IDs retain normal initial routing; this cache cannot restore upstream history, and requests replaying only encrypted input without `previous_response_id` do not use this binding. Provider-specific state rejection still needs the bounded thinking fallback or client recovery; this feature does not guarantee a reply during an upstream outage.

Deployment recommendation: start `RESPONSES_RECOVERY_PREAMBLE_LIMIT_KB=1024` for large echoed tool payloads, budget concurrent buffering, and verify two successive requests reach the same single-key channel. No live deployment or environment change is performed by the local patch. Roll back continuation routing by restoring the prior image; the stream-recovery flag alone does not disable it. Cached bindings expire after one hour.

The bounded thinking fallback uses the native DeepSeek adaptor's `reasoning.effort=none` mapping for V4 and V4.1, including `deepseek-v4.1-flash`, on an error-only HTTP 400 naming the missing reasoning state. The fallback and model-suffix conversion share version-bounded matching: `deepseek-v40-*`, `deepseek-v4.10-*` and unrelated aliases are excluded. It retries once with the same adaptor and credential, after parameter overrides, preserving the complete input and `previous_response_id`. It does not retry SSE failures, reported usage, committed output, or arbitrary OpenAI-compatible aliases. The user reports group 1 now uses DeepSeek channels; real upstream acceptance of thinking-off still requires a post-deployment check. `max_output_tokens` incomplete responses are not replayed and their limit is not silently raised.

Release plan (2026-09-19): deploy only the independent `cf-global-newapi-official-B` service using the immutable image built from this branch. No new SQL, schema changes, manual Redis writes, or environment changes are required; application continuation-cache writes are described above. The preamble limit remains 256 KiB unless separately configured. Preserve the prior deployment `6aaba5b3fa283769e51c1a00` as the rollback target. Verify the new deployment, public health endpoint, build version and startup logs before declaring release success; live continuation and thinking-fallback checks remain separate from health checks.

DeepSeek channel URL note: its Responses adaptor appends `/responses`, not `/v1/responses`. For the user's compatible upstream requiring `/v1/responses`, the user confirmed that a base URL ending in `/v1` passes the channel test. Do not apply this change indiscriminately to Chat requests: that adaptor appends `/v1/chat/completions` and could duplicate `/v1`.

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
