---
kind: verification_report
date: 2026-10-28
---

# Default Task Center deployment

The canonical service origin is <https://app.openpero.com>. It shares the existing
AWS machine with DSH through its own Nginx vhost. The website domain's DNS was not
changed. The Go control plane and PostgreSQL have separate containers, credentials,
network and persistent database storage. Workers execute on other machines.

The initial server release was `20261028T114417Z-c000808b`, built locally from
committed source `c000808b85f99248e492dcf9df633585ff33df07`. No source build ran
on the small AWS machine. See the [runbook](../../deployment/task-center/README.md)
for deployment, credentials, resource limits, backups, certificate renewal and
rollback. The PostgreSQL image is fixed to 17.11 Alpine and its observed digest.

## Verified facts

- HTTPS `/healthz` returned 200 with database connectivity; unauthenticated task
  API requests returned 401. The HTTP vhost redirects to HTTPS and preserves the
  existing ACME challenge route.
- The signed certificate is for `app.openpero.com`, expiring 2027-01-26. A targeted
  Certbot staging renewal succeeded. A separate daily renewal command and daily
  database backup are installed in `/etc/cron.d/openpero`.
- Public `phodal/pero` intake imported 413 Tasks: two GitHub Issues and 411
  ROADMAP checkboxes, including 400 unchecked and 11 checked. All source Tasks
  remained undispatched. The project-scoped Client saw the same complete count
  as the operator; source sync reported complete coverage and no errors.
- A separate `deployment-acceptance` project exercised public HTTPS dispatch,
  a local independent Rust Worker, scripted ACP output, persisted events,
  completion and running cancellation. Its Tasks are hidden from the `pero`
  Client. Repeated verification attempts retain their explicitly labeled fixture
  Tasks/Runs; they do not contribute accepted work or community points.
- The normal local Desktop profile saved the `Phodal Desktop` Client credential
  through the Host and OS encryption. After closing and relaunching without cloud
  environment variables, it loaded the `pero` scope and opened Remote tasks.
  The plaintext token was not written to runtime preferences or diagnostics.
- Desktop now uses this service as its default connection origin. A blank URL
  accepts that default when the user explicitly saves a token; saved custom
  connections and explicit disconnects retain priority.
- Full Go race tests ran against a real isolated PostgreSQL database and passed;
  `go vet`, the complete generated-contract/theme/locale checks, the Wasm/web build
  and the actual Desktop encrypted-settings suite passed.

## Operational limits

GitHub login is deferred by user choice. Desktop uses project-scoped Pero Client
tokens. A production Worker is not registered to `pero`; actual source execution
requires provisioning and configuring an approved Worker. Fixture success proves
the deployed dispatch protocol, not model-provider behavior or business acceptance.

The public `/tasks` page remains an explicitly published community board. Source
import does not automatically publish Tasks there. Authorized Desktop/API users
can browse the full source inbox. No GitHub writes, source-task executions,
automatic dispatch, PR publication, task acceptance or points were created.

Backups are stored on this host; off-host disaster recovery is not established.
The shared host still has limited RAM/disk and an old operating system. This
deployment is a working first installation, not Kubernetes, sandboxed code
execution, a signed Desktop release or a load-tested multi-tenant service.

## Reproduce

Run Electron checks sequentially because they use native input focus.

```sh
PERO_DEPLOYMENT_E2E=1 npm run test:deployment
npm run test:remote-settings
```

The opt-in public suite reads private operator/client files from
`~/.config/openpero/task-center`, compares every source page with pinned GitHub
content, executes only its dedicated fixture project, and checks actual Desktop
input, encrypted restart, ROADMAP pagination, narrow/dark layouts and the public
community pages. Generated evidence lives in `test-results/deployment/` and is
ignored by Git. Credentials and binary captures are not committed.

During verification, the harness initially observed default connection metadata
before the canvas had painted the form. Waiting for rendered animation frames
before native input fixed the missed click. Public pagination also needs a longer
observable loading bound than a local fixture; the suite waits for loading to
finish rather than pausing for a fixed duration.

## Functional task discovery deployment — 2026-10-29

The final server release is `20261029T012052Z-ed086f6f`, built from committed
source `ed086f6f40bbf0b7e41880230da897f4ed0b44f8` and deployed through the existing
runbook. The public Roadmap publications identify source commit
`70c6d1ff93316619bc71398d96ab723982b6d47f`. Server code and imported source are
separate revisions. The deployment retained OAuth/configuration, took its normal
pre-deployment dump and kept the previous healthy container. No migration was
added. The final container was running with zero restarts and HTTPS health 200.

The live browser suite passed 19 checks against the real HTTPS service at
`2026-10-29T01:27:18.250Z`:

- 252 current Roadmap tasks match every committed title, line and functional
  path: 12 domains, 48 feature groups and 33 checked sources. Three public Issues
  remain under the final “其他任务” category. The browser displays 255 total tasks,
  with 221 unfinished by default; historical missing sources remain outside this
  current outline. Issue #2 was closed after integration/public delivery and synced
  as a source closure, without Run acceptance or points; sync reported one update,
  zero dispatches, complete coverage and no errors.
- Domain → feature → task keyboard navigation, source/Desktop links, separate
  inline scope/acceptance, title/acceptance search, ancestor reveal, complete-state
  access, panorama feature drill-down, empty recovery and URL/selection restoration
  all passed. Functional domains precede unclassified Issues.
- Both views passed at 390px in light and dark themes without horizontal page
  overflow. Native disclosures remained usable without JavaScript. No page errors
  were observed; unauthenticated API access returned 401 and OAuth redirected to
  GitHub without performing a login.
- Before/after browser verification retained 899 total task records, 11 Runs,
  the existing one `pero` Run association and zero point entries. The digest of
  all task/Run links was unchanged. This check created no login, claim, execution,
  acceptance, points or GitHub write.

The final grouping adjustment also passed `go -C server test -race ./...` with
isolated PostgreSQL and the existing community browser contribution regression
(publication, fixture login/Worker, claim, execution, review/scoring and source
changes). These local fixtures are distinct from live browser discovery; a real
Agent run, OAuth round trip and PR publication were not part of this deployment
acceptance.

Reproduce live discovery with:

```sh
PERO_DEPLOYMENT_E2E=1 \
PERO_TASK_BROWSER_REVISION=70c6d1ff93316619bc71398d96ab723982b6d47f \
node tests/e2e/task-browser-live.mjs
```

The optional revision checks the pinned publication source. Read the source link
before selecting a newer revision. Screenshots, the 19 checks and the database
baseline comparison are in `test-results/task-browser-live/` and remain ignored.

## Local Worker enrollment rollout — 2026-10-29

The approved server rollout activated release
`20261029T075057Z-8b55dd83`, built from exact committed revision
`8b55dd835587cc8c9fdbadba2eae3548f45fd032` through the existing backup and
health-checked deployment script. The previous live revision was
`ed086f6f40bbf0b7e41880230da897f4ed0b44f8`; its Client API did not implement the
Desktop device-enrollment POST. No unrelated server or deployment changes were
included in this upgrade.

Observed live receipts:

- `/opt/openpero/current`, its `source-revision` file and the running container's
  OCI revision label all identify `8b55dd835587cc8c9fdbadba2eae3548f45fd032`.
- The deployed Linux amd64 binary SHA-256 is
  `ff32f31ae0800d2d24b73b77438407919be4e6d73d6a8b5525ed8f706922a747`, matching
  the separately prepared, committed-source archive build.
- The container was running with zero restarts. Public HTTPS `/healthz` returned
  200 with database status `ok` at `2026-10-29T07:53:37Z`.
- The normal pre-deployment database backup exists at
  `/var/lib/openpero/backups/20261029T075057Z-8b55dd83.dump`. Existing OAuth,
  credentials, repository bindings and deployment resources were retained.
- Migration version 14 is recorded. `worker_devices` and
  `worker_enrollment_receipts` both exist and each had zero rows at verification.
  This additive migration did not enroll a device, grant project access, dispatch
  a task or execute an Agent.

The same server source tree passed the full local PostgreSQL race suite before
rollout; the deployed artifact is a statically linked Linux build. Deployment
acceptance here proves the matching server and migration are available. Actual
Desktop enrollment recovery and local execution need their own user/device
receipts; no enrollment POST, task dispatch or grant change was performed during
this rollout verification.
