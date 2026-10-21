# Synthetic v1 conformance corpus

All examples are synthetic. No token is usable; `.invalid` is an example host.
Run the single checker described in [verification evidence](../../docs/evidence/issue-3/verification.md).

| File | Classification | Executed now | Future proof |
| --- | --- | --- | --- |
| `schema-cases.json` | schema | Positive validation and negative keyword/instance-path matching against named OpenAPI components | Actual request/response boundary enforcement |
| `response-cases.json` | schema + tooling | Body shape, media/status and generation/replay/cache/Location/challenge example consistency | Runtime conditional header behavior |
| `problems.json` | schema + tooling | Go problem examples, all stable anchors, titles/statuses and type URIs | BFF consumer tolerance, sanitized detail, outcome determination |
| `domain-cases.json` | domain | Budget shape and independent integer-millionth example arithmetic/currency checks | Actual Go/BFF integration, immutable stored currency |
| `raw-cases.json`, `raw/*` | raw transport | Inventory and SHA-256 byte integrity only | Go/BFF raw duplicate headers/decoded keys, malformed/trailing JSON, upstream rejection |
| `state-cases.json` | state/security | Classification, acceptance references and complete Given/When/Then inventory only | Auth, isolation, transactions, replay, concurrency, retention, restart, recovery |

Every case maps to acceptance criteria. Negative schema cases identify a JSON
Schema keyword and instance path; arbitrary failure is not sufficient.
Domain checks validate **example consistency**, not an alternate production domain.
Currency-mismatch examples deliberately pass structural schemas before failing
the separate semantic check.

Raw JSON is stored as `.json.txt` to discourage automatic JSON normalization.
Do not parse/reformat it before a future boundary test. Raw HTTP samples preserve
CRLF and duplicate field lines; the future harness supplies valid synthetic test
authentication/current state so the intended invalidity is isolated. The
`SYNTHETIC_NOT_A_TOKEN` marker is not a JWT. Upstream samples apply to BFF response
validation. Hash checks prove preservation, never runtime rejection.

All state/security cases and raw boundary executions are explicitly **NOT RUN**.
In particular, OpenAPI/schema tooling cannot prove duplicate detection, token
verification, cross-field currency equality, calculated totals, owner isolation,
atomic commits, outcome knowledge or recovery fencing.
