# Explicit chat-completions provider

This is the text/tool-call slice of the pinned DSH Go port. Configure endpoint,
credential, provider ID and exact model catalog explicitly. No default provider,
model discovery, credential lookup, anonymous identity, session-log telemetry,
Files API upload, UI, or Point service is included. HTTP is accepted only for
numeric loopback fixture servers; deployment endpoints require HTTPS. Redirects
and SDK retries are disabled. Retry decisions remain owned by the harness.

Unknown model/provider, media, content extensions, request extensions and
unadvertised reasoning settings fail before network dispatch. The provider
supports streamed text/reasoning, tool-call argument assembly, tool results,
usage, cancellation and structured provider failures. It rejects truncated SSE,
missing terminal finish reasons and incomplete tool calls. Limits are 1 MiB per
SSE line/buffer and 16 MiB per response. Error bodies and remote request IDs are
not copied into public diagnostics.

The transport/assembly and error taxonomy are adapted from the source recorded
in SOURCE.json; the explicit constructor and text-only request encoder are a
narrow replacement for product-specific initialization and media routing. The
upstream DeepSeek notice is retained in DEEPSEEK_LICENSE.txt. Tests use only local
HTTP servers and literal fake credentials. They do not certify real models.
