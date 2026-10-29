# Explicit runtime configuration seams

Generic settings and credential stores depend only on the extracted runtime and
LLM error types. Import paths are local to this module. This package does not
bootstrap a Point account, UI or service. `LoadLayeredEnvironment` requires an
explicit `Home`: automatic DSH_HOME / ~/.dsh discovery is removed. Callers decide
whether to use dotenv layers at all; the headless entrypoint must not invoke
unrelated user/project configuration discovery implicitly.

SOURCE.json records pinned provenance. The focused test verifies the explicit
environment boundary. Full upstream settings/credential regression coverage has
not been transplanted; module integration tests are separately required.
