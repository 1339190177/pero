# Opt-in live DeepSeek smoke check

No-key refusal (must exit 2 before filesystem reads or networking):

```sh
env -u DEEPSEEK_API_KEY node tests/runtime/live-deepseek/run.mjs
```

Build the existing Worker, budget-enabled `pero-agent-acp`, and the test-only
`live` binary from `tests/fixtures/runtime-tool-client`. In an authorized process
with `DEEPSEEK_API_KEY` already set, invoke the runner with absolute `--agent`,
`--worker`, `--client` paths the approved `--agent-sha256`, and mandatory `--scene worker` or `--scene runtime`. Do not place the key
in CLI arguments, Agent JSON, repository files or receipts.

The runner uses the previously verified official model `deepseek-flash`; it makes
no additional discovery request. Generation uses the fixed official endpoint; no redirects or endpoint override. This explicitly approved run uses only the existing
HTTP_PROXY/HTTPS_PROXY/NO_PROXY and SSL_CERT_FILE/SSL_CERT_DIR configuration;
loopback exclusions are appended, TLS verification remains enabled, and production
defaults remain direct unless --use-env-proxy is specified. One explicitly selected
scene runs: unattended Worker reads a fresh random fixture; the existing Rust ACP
runtime grants exactly one matching write and one `pwd` call, verifies the read
back and working directory. Unknown/repeated calls and other shell commands are
denied. The Worker permission policy is unchanged. Cloud is still a loopback
fixture, not production Cloud.

Each Agent is configured for at most four generation requests, 512 output tokens
per request, no retries and explicit `thinking: {type: "disabled"}` via `--disable-thinking`. Stage deadlines are 55 seconds for Worker, and 60 seconds in the Rust client (65-second
outer deadline). Failed generation calls count against the budget. Receipts omit
raw model responses, credentials, HTTP bodies and environment dumps. Temporary
non-secret configuration and session data are removed on normal/error completion.
Do not externally kill the Node parent: its internal deadlines perform owned-child
cleanup; abrupt termination cannot guarantee cleanup. Shell remains explicitly
opt-in and is not an OS/network sandbox.

A successful local fake-provider dry run only validates the runner and permissions;
it is not evidence that a real model has passed. Live results must name exact
binary hashes and observed tool receipts independently.

The Chat Completions thinking toggle follows the dedicated official
[thinking-mode guide](https://api-docs.deepseek.com/guides/thinking_mode/);
this test does not rely on `reasoning_effort=none` to disable thinking.
A prior live attempt reached models discovery but failed Worker tool verification;
its coarse receipt cannot establish the cause. The runner now records only
whitelisted event/tool/status summaries, nonce-match booleans and HTTP categories.
It does not print model text or raw tool arguments. No automatic retry follows
failed verification. Each invocation permits one scene with at most four calls;
operators must account for earlier attempts before authorizing another invocation.

## Observed live result and test correction

An earlier bounded Worker attempt observed one completed `read_file`, matching
fixture path and nonce, a successful Cloud-fixture completion, and `end_turn`
with no session errors. Its original overall receipt remains **failed**: the
workspace assertion incorrectly rejected Worker’s normal `.pero-worker.lock`.
The assertion now requires exactly the untouched source and a regular, private,
empty, current-owner lock file; extra files and symlinks still fail. A real
Worker/Go/local-model regression validates the correction without paid calls.
At that stage the corrected script had not been rerun against DeepSeek and no
real-provider write/shell scene had been executed. Do not rewrite the original
receipt as passed; the later separately authorized runs are recorded below.

Reproduce that correction without credentials:

```sh
node tests/runtime/live-deepseek/worker-local-check.mjs /absolute/pero-worker /absolute/pero-agent-acp
node --test tests/runtime/live-deepseek/diagnostics.test.mjs
```

## Subsequently authorized live verification — 2/2 passed

On 2026-10-28 at 12:32:56 UTC, a separately authorized additional budget of
at most eight generation requests completed both official DeepSeek
`deepseek-flash` scenes successfully (at most four requests per scene,
512 output tokens per request):

- **Worker read:** one `read_file` completed, path and nonce matched, source
  remained unchanged, only the source and valid private lock were present,
  Worker completion succeeded, and the model returned `end_turn` with no session
  errors. No permission grants were needed.
- **Direct Rust ACP client:** exact allow-once approvals enabled the prescribed
  file write/readback and fixed `pwd` command; both checks passed with two grants
  and zero denials. This was not an unattended Worker write/shell grant.

Both runner receipts passed and executable hashes matched before and after each
scene. The earlier failed receipts remain failed; these are separate successful
runs after the test correction. Cloud remained a loopback contract fixture,
not production Cloud/PostgreSQL. Approval was limited to temporary test files
and the fixed safe command; it does not change Worker default denial or make
shell an OS/network sandbox. No further provider calls were made.

Verified executable SHA-256 identities:
- Go Agent: `ec85b34c4a9fa97748d7bad9ff0c481c3eb8677caa0510bc7ebbd6c0573fb373`
- Rust Worker: `9a071a14d7d7d637ebe2b24cc8dc813218cb03ae0147dadd9ec5d558d27e5c1c`
- Direct Rust ACP test client: `5eb7d85429168f9f28efe672996e9aa02032adab54755bbd93cd45b4ba99ed40`
