# Go ACP runtime → Rust Worker fixture verification

This opt-in check runs the real `pero-agent-acp` and `pero-worker` executables.
Both HTTP services are loopback test fixtures: the provider returns scripted
OpenAI-compatible SSE and the control plane implements the existing Worker wire
contract. There are no real model requests, credentials, deployment or UI.
The process-supervision cases require a Unix host with `/bin/ps` and Node.js.

After building the production executables using the repository's supported build
environment, build the permission-only fixture against the same Go module:

```sh
go -C harness/go build -o /tmp/pero-acp-permission-fixture \
  "$PWD/tests/fixtures/go-acp-permission/main.go"
node tests/runtime/go-acp-worker.mjs \
  /absolute/path/to/pero-worker /absolute/path/to/pero-agent-acp \
  /tmp/pero-acp-permission-fixture
```

The permission executable is test-only: it uses the real ACP server, provider and
core and registers one explicitly approval-guarded no-op. Its private marker
would record any execution; the test requires that marker to remain absent.
The production entrypoint does not install this fixture tool or a fixture flag.

The suite checks:

- Real streamed completion is journaled/uploaded and one model turn is submitted.
- Cancellation aborts the provider HTTP request and cannot report success.
- Killing the actual Go process produces a failed result and cleanup.
- A lost completion response retains the same receipt, which is retransmitted
  after restart without replaying the prompt.
- A success receipt rejected after Cloud cancellation becomes reconciliation,
  rather than overriding the authoritative cancellation.
- An interrupted claim uses its original identity and reconciles without a prompt.
- Lease loss stops the runtime and leaves a reconciliation outcome.
- An unattended ACP permission request is denied; the guarded tool never runs.

Each run uses fresh private directories and an explicit environment allowlist.
Synthetic credential markers must not appear in uploaded Worker events. The
script hashes all executables before and after the suite and rejects a changed
binary, so a rebuild requires rerunning verification.

This is process/protocol integration, not production acceptance. The fixture does
not prove PostgreSQL concurrency, multi-tenant authorization, lease enforcement by
the deployed control plane, real-provider compatibility, session resumption,
untrusted-code sandboxing or arbitrary MCP tool approval. In particular, only an
explicitly guarded tool is covered by the permission test; approval policy is not
claimed as a blanket sandbox for all future tool adapters.
