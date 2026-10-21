# Go Budget v1 semantic contract

Contract revision 1, authored by Codex for [budget-service #3](https://github.com/finance-lab-project/budget-service/issues/3).
The authoritative Go wire shapes are in [OpenAPI 3.1.1](../api/openapi.yaml).
This companion defines semantics, stable problem identifiers, browser mapping,
and future runtime conformance. It does not establish a running API.

## Baseline, decisions and ownership

The [captured issue revision 2](evidence/issue-3/source.md) resolves Stage 0
D1–D3 and approves the intended token profile. The source-contract revision is
[`6c7e39758fda11c2a9679feba6cc2d32e14182e3`](https://github.com/anton415/finance-lab/tree/6c7e39758fda11c2a9679feba6cc2d32e14182e3):
[Stage 0](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/go-budget/stage-0-specification.md),
[ADR-024](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/architecture/decisions/0024-openapi-contract-first-rest-versioning.md),
[ADR-022](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/architecture/decisions/0022-exact-decimal-money-currency-contract.md),
[ADR-014](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/architecture/decisions/0014-keycloak-service-to-service-security.md),
and [ADR-030](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/architecture/decisions/0030-release1-go-migration-implementation-sequence.md).
The unchanged pure-domain baseline is
[`a964c0693fdd475a8a916796fc65f22faa7c0e74`](https://github.com/finance-lab-project/budget-service/tree/a964c0693fdd475a8a916796fc65f22faa7c0e74/internal/budget).

| Decision | Approved choice | Alternative and tradeoff |
| --- | --- | --- |
| D1 | Repository-hosted stable problem identifiers and fixed Go/BFF catalogs below. | Waiting for a documentation domain delays the initial contract. GitHub identifiers require preserving the existing URIs if documentation moves. |
| D2 | External recovery generation; refuse fresh revision overflow. | Revision-only checks cannot identify restored history. Resetting/wrapping revisions permits unsafe writes; generation adds operator coordination without recovering lost data. |
| D3 | Exact header grammars and structured validated-value equality. | Raw-body hashes distinguish irrelevant formatting; canonical-byte encoding adds a separate format. Structured equality requires retaining the complete binding and original key. SHA-256 indexes keys only. |
| Token profile | Narrow user-delegated access JWT, sole Budget audience and trusted account-derived user principal claim. | Generic service tokens, editable user attributes or blanket user labeling allow confused identity/privilege expansion. The approved interface still requires actual Keycloak issuance/downscoping proof. |

Go owns service shapes, domain errors and future authorization/storage behavior.
BFF owns its browser contract, sessions, CSRF/origin checks, delegated tokens and
strict boundary validation. React preserves exact strings/nulls, drafts and
pending operations; it owns neither authoritative arithmetic nor downstream
bearer tokens. Keycloak/platform owns trusted issuance and operational configuration.

D4 issuance proof, D5 PostgreSQL/Goose selection and D6–D8 session, timeout,
retention cleanup, generation distribution, writer fencing and draft-preservation
integration remain later gates. They do not block authoring this contract.
No HTTP handler, runtime decoder, auth middleware, persistence adapter, migration,
Keycloak configuration, BFF/React implementation, SDK or deployment is included.
Legacy V2 backup and Spring behavior stay outside this issue.

## Representations and validation [schema; domain; raw transport]

Go exposes GET/PUT `/api/v1/budgets/{month}`; BFF maps these to
`/api/web/v1/budgets/{month}`. Month is complete ASCII `YYYY-MM`, years
0001–9999, months 01–12, without trimming or timezone conversion.

PUT requires `application/json`, one `Idempotency-Key`, one `Budget-Generation`
and exactly five required body fields: `expectedVersion`, `currency`, `income`,
`spending`, `investments`. No owner, month, totals or backup format version is
accepted. Spending requires exactly the 15 IDs in OpenAPI and `SpendingCategoryIDs()`.
Every planned value is explicit null or an object containing exactly `amount`
and `currency`; omission is invalid. Null is unplanned; Money `"0"` is planned zero.

Only RUB/USD/EUR are accepted. All populated Money, including zero and computed
totals, must match the immutable budget currency. Schema checks allowlisted
currencies, but cross-field equality and immutable-state checks are separate.

Source amounts are nonnegative canonical ASCII strings: integer `0` or 1–18
digits starting in 1–9, optionally a fraction of 1–6 digits ending nonzero.
Maximum: `999999999999999999.999999`. Reject signs, exponent notation, leading
integer zeros, trailing fractional zeros, whitespace and excess range/scale.
Never trim, normalize, round or convert authoritative values through floats.
Patterns use an absolute end assertion so a final newline is rejected.

The four totals are always Money, never null. Null contributes exact zero.
Remaining = income − spending − investments. Totals allow 20 integer digits
and six fractional digits; only remaining may be negative, never negative zero.
The fixture `maximum-aggregate-negative` sums fifteen maximum spending inputs
to `"14999999999999999999.999985"` and subtracts another maximum investment to
produce `"-15999999999999999999.999984"`. Exact tenths sum to `"0.3"`.

Creation has `expectedVersion: null`; updates use a positive canonical decimal
string at most `"9223372036854775807"`. JSON numbers, zero, signs, leading zeros,
fraction/exponent notation and overflow are invalid. The response version is
always a string. REST major version, OpenAPI format version, budget revision,
database migration and legacy backup format version are distinct.

Go and BFF must reject malformed/trailing JSON and duplicate **decoded** keys at
every object level before ordinary parsing loses evidence. `"currency"` and
`"\u0063urrency"` are duplicates. Unknown/missing properties and wrong types are
also rejected. BFF validates upstream responses, including nested totals.
Raw fixtures preserve these bytes; no runtime boundary test is claimed here.

## Identity and permissions [state/security; raw transport]

Require exactly one `Authorization: Bearer` header over TLS. Accept only
Keycloak-signed access JWTs, never ID or refresh tokens. Each environment
configures one explicitly trusted realm issuer with exact matching and trusted
issuer JWKS discovery/configuration; this contract does not fabricate its URL.
The algorithm allowlist is RS256 only.

Audience is solely `finance-lab-budget`, as a string or a one-element array.
Owner is the exact verified nonempty `(iss, sub)` pair, never email, client ID,
`azp` or a body-supplied owner. Require trusted `principal_type: "user"`, derived
from the subject account during trusted issuance. An editable attribute or a
mapper that labels all exchanged tokens as users is insufficient. Machine and
client-credentials identities cannot impersonate end users.

GET requires `budget:read`; PUT requires both `budget:read` and `budget:write`.
Extra claims/scopes grant no other Budget privileges. Require valid `iat` and
`exp`, a token lifetime at most 300 seconds, and valid `nbf` when present.
Allow 30 seconds of clock tolerance for time validation; this does not increase
the allowed issued token lifetime.

Missing/invalid/expired tokens, invalid signature/algorithm/issuer/audience/time
profile, or missing subject produce 401. A valid identity without eligible user
principal type or required permissions produces 403. Every request, including
replay, authenticates and authorizes before private-data access.

Missing credentials challenge:
`Bearer realm="finance-lab-budget"`.
Invalid/expired token challenge:
`Bearer realm="finance-lab-budget", error="invalid_token"`.
Insufficient scope uses `error="insufficient_scope"` with
`scope="budget:read"` for GET or `scope="budget:read budget:write"` for PUT,
along with that realm. An ineligible principal is not an insufficient-scope
failure. Browser authentication errors are BFF-owned.

If a required uncached JWKS lookup is unavailable, fail closed with 503 rather
than misclassifying the token as invalid. An acceptable cached key needs no
live Keycloak call.

BFF uses Keycloak standard token exchange V2 for a delegated access token
preserving the user's subject, with only the Budget audience and required
scopes. React never receives that token. Actual realm/client version,
trusted-claim provenance, sole audience, lifetime, exchange and effective
downscoping/absence of privilege expansion are **NOT RUN**, pending integration.
Requesting a scope alone is not proof of downscoping.

## Save, replay and recovery [state/security; domain]

Required processing order:

1. Authenticate and authorize.
2. Validate request syntax and values.
3. Validate current generation.
4. Resolve owner and inspect a matching retained receipt.
5. For a fresh operation, check expected version, then immutable currency, then revision capacity.
6. Atomically commit budget, version and successful receipt; acknowledge only after commit.

Create returns 201, version `"1"` and creation Location. Update returns 200 and
increments once, including unchanged values. Both return the complete Budget.
GET returns the complete current own Budget; absent own month is 404 with
`month`, creating neither budget nor owner row.

An expected-absence/version mismatch returns `BUDGET_VERSION_CONFLICT` with the
complete current **own** Budget or null if absent. An attempted currency change
after a matching version check is `INVALID_REQUEST`. Fresh update at the maximum
revision is `BUDGET_VERSION_EXHAUSTED`, with GET still available. These rejected
fresh checks change neither budget, version nor successful receipt. Preserve the
draft and require operator intervention at exhaustion; never reset/wrap a version
or suggest a new key as the remedy. A retained successful replay still succeeds
at the limit without incrementing.

Scope exact keys to the verified owner **across months**. Index with SHA-256 of
the exact accepted ASCII key, retaining and comparing the original key and full
binding. A digest collision fails closed with 500 `INTERNAL_ERROR` and no mutation.
The binding is structured JSON value equality over `apiMajor` (v1), `method`
(PUT), `month`, `generation`, and the complete validated body. Compare decoded
strings/nulls exactly; ignore object order and insignificant whitespace.
Equivalent escapes bind equally; reject duplicate decoded keys first.

Changed month, expectedVersion, amount, currency, null versus zero or generation
changes binding. Generation validation still precedes receipt lookup.
Token bytes, expiry and `jti` are excluded; a renewed valid token for the same
verified owner may retry. No body digest or canonical byte format is required.

A retained matching receipt wins before fresh version/currency checks and
returns the original success status/body/creation Location. A retained exact key
with a different binding returns `IDEMPOTENCY_KEY_REUSED`. Concurrent duplicates
coordinate through database uniqueness. After rollback a waiter may perform
normal fresh checks; after commit it replays. The configurable coordination wait
starts at five seconds, not an end-to-end SLA. Unresolved wait returns
`SAVE_RESULT_UNAVAILABLE` with `saveOutcome: "unknown"` and no second write.
Budget/version/successful receipt share one transaction; no durable pending receipt.

Retain successful receipts for at least 24 hours **after commit**. Deliberate
client retries are allowed only within 24 hours **of first send**; retries do not
reset that deadline. Preserve an immutable pending operation—key, method/month,
generation, exact body, first-send time, identity context—separately from later
draft edits. Never silently retry as a different signed-in owner. After the
deadline preserve drafts, GET current state and require review before a new
operation/key. After cleanup, receipt absence cannot prove prior failure or
detect all historical changed-content reuse.

A replay may return version `"8"` when current state is `"9"`: confirm the earlier
operation, refresh current state, and preserve newer edits. No automatic PUT retry.
Only `BUDGET_VERSION_CONFLICT` enters the UI conflict-review flow.

Generation lives outside the restored Budget database. Initialize once and
retain it across normal restart and compatible application rollback. Before
reopening writes after historical restore, fence old writers and install a fresh
generation. A well-formed mismatch returns `BUDGET_RECOVERY_REQUIRED` before owner
creation, receipt access or mutation. Missing/malformed/repeated generation is 400.
Never repair an old pending operation by silently replacing its generation.
Preserve drafts, GET state and require review before a new operation/key.
Historical restore may lose acknowledged writes and receipts within the RPO;
generation does not recover that data. Session invalidation, fencing and restore
evidence remain later operations/integration work.

Concrete Given/When/Then traces for creation, update, create-again, stale version,
currency ordering, limit replay, concurrent outcomes, ownership and recovery are
in [state-cases.json](../fixtures/api-v1/state-cases.json). Their execution is **NOT RUN**.

## Header matrix and browser mapping [schema/tooling; raw transport; state/security]

| Header | Complete parsed field-value grammar |
| --- | --- |
| `Idempotency-Key` | 1–128 ASCII `[A-Za-z0-9._:-]` |
| `Budget-Generation` | 1–128 ASCII `[A-Za-z0-9_-]` |
| `Idempotency-Replayed` | Exactly `true` or `false` |

Header names are case-insensitive; key/generation values are case-sensitive.
Reject repeated key/generation headers even when identical, comma lists, quotes,
embedded whitespace and other characters. HTTP parsing already removes framing
outer optional whitespace; validate the parsed field without further trimming.
Do not claim application detection of whitespace already removed by the parser.
Recommend UUIDv4 for new keys/operator generations; accept every grammar-valid value.

| Response | Budget-Generation | Idempotency-Replayed | Location |
| --- | --- | --- | --- |
| GET 200 | Required, current | Omitted | Omitted |
| Authenticated GET 404 | Required, current | Omitted | Omitted |
| PUT 200 | Required | `false` original / `true` replay | Omitted |
| PUT 201 | Required | `false` original / `true` replay | Required, original creation location |
| 409 BUDGET_RECOVERY_REQUIRED | Required, current | Omitted | Omitted |
| All other errors | Omitted | Omitted | Omitted |

All successes/problems require `Cache-Control: no-store`. Success media type is
`application/json`; errors use `application/problem+json`.
OpenAPI declares headers per status, so its 409 generation header is conditionally
required only for the recovery code. The fixture checker checks that matrix on
examples, not actual HTTP responses.

BFF preserves key/generation values exactly and forwards valid documented Go
status/body/type and contract headers. For creation, including replay, rewrite
Location from `/api/v1/budgets/{month}` to `/api/web/v1/budgets/{month}`.
If cross-origin access is configured, explicitly allow/expose relevant headers.

| BFF condition | Browser result | Forwarding/outcome |
| --- | --- | --- |
| Missing/expired/invalid session | 401 AUTHENTICATION_REQUIRED | Do not forward |
| CSRF/origin failure | 403 CSRF_REJECTED | Do not forward |
| Required session store/token exchange unavailable | 503 SERVICE_UNAVAILABLE | Fail closed |
| Unexpected delegated Go 401/403 or malformed upstream response | 502 UPSTREAM_ERROR | Web-owned problem; unknown marker for forwarded PUT unless outcome established |
| Timeout after forwarding PUT | 504 SAVE_OUTCOME_UNKNOWN | Required unknown marker |
| Valid documented Go success/problem | Preserve status/body/type and contract headers, except Location rewrite | Add unknown marker when required by unresolved forwarded-write outcome |

Never translate ambiguous write outcomes into definite save failure or automatic
retry. A current refusal does not describe the complete history of the key.

## Problem catalog [schema/tooling; state/security]

Use RFC 9457 `application/problem+json`. Required fields are `type`, fixed English
`title`, numeric `status` equal to HTTP status, and stable `code`. Optional `detail`
is sanitized and never machine-parsed. Optional `saveOutcome` can only be `"unknown"`;
it is required on unresolved duplicates and any other PUT failure whose commit
outcome cannot be established, including ambiguous internal/upstream failures.
Its absence is **not** proof that an earlier attempt failed.

Producers emit only documented fields. The OpenAPI variants check this producer
shape; consumers must tolerate unknown problem extensions rather than applying
the closed producer schema as an extension-rejection policy.
No `instance` or other extension is currently documented.

Stable prefix:
`https://github.com/finance-lab-project/budget-service/blob/main/docs/api-v1-contract.md#`.
Use `budget-v1-` for Go and `web-v1-` for BFF-owned problems. Forwarded Go problems
retain the original URI. These identifiers stay stable even if documentation moves.
The anchors and exact titles below are frozen by this revision.

### Go problems

| Code / stable anchor | Status | Fixed title | Meaning / required extensions |
| --- | --- | --- | --- |
| <a id="budget-v1-invalid-request"></a>`INVALID_REQUEST` | 400 | Invalid request | Invalid month, body, Money, revision, key or generation; currency change after matching version. Required extensions: none. |
| <a id="budget-v1-authentication-required"></a>`AUTHENTICATION_REQUIRED` | 401 | Authentication required | Missing, invalid or expired token, including an invalid token profile. Required extensions: none. |
| <a id="budget-v1-forbidden"></a>`FORBIDDEN` | 403 | Forbidden | Valid identity lacks the required permissions or eligible user principal type. Required extensions: none. |
| <a id="budget-v1-budget-not-found"></a>`BUDGET_NOT_FOUND` | 404 | Budget not found | No budget for this authenticated owner and month; no budget or owner row is created. Required extensions: `month`. |
| <a id="budget-v1-budget-version-conflict"></a>`BUDGET_VERSION_CONFLICT` | 409 | Budget version conflict | Expected absence or version differs from the current own budget. Required extensions: `currentBudget`. |
| <a id="budget-v1-idempotency-key-reused"></a>`IDEMPOTENCY_KEY_REUSED` | 409 | Idempotency key reused | Retained exact key has a different validated binding. Required extensions: none. |
| <a id="budget-v1-save-result-unavailable"></a>`SAVE_RESULT_UNAVAILABLE` | 409 | Save result unavailable | Concurrent duplicate remains unresolved after the coordination wait; original operation may still commit. Required extensions: `saveOutcome`. |
| <a id="budget-v1-budget-recovery-required"></a>`BUDGET_RECOVERY_REQUIRED` | 409 | Budget recovery required | Well-formed generation differs from current generation; refused before owner creation or receipt access. Required extensions: none. |
| <a id="budget-v1-budget-version-exhausted"></a>`BUDGET_VERSION_EXHAUSTED` | 409 | Budget version exhausted | A fresh update would exceed the maximum revision; operator intervention is required. Required extensions: none. |
| <a id="budget-v1-unsupported-media-type"></a>`UNSUPPORTED_MEDIA_TYPE` | 415 | Unsupported media type | PUT requires application/json. Required extensions: none. |
| <a id="budget-v1-internal-error"></a>`INTERNAL_ERROR` | 500 | Internal error | Unexpected internal failure; never fabricate a successful acknowledgment. Required extensions: none. |
| <a id="budget-v1-service-unavailable"></a>`SERVICE_UNAVAILABLE` | 503 | Service unavailable | Required dependency unavailable or operator write suspension. Required extensions: none. |

### BFF-owned problems

| Code / stable anchor | Status | Fixed title | Meaning / required extensions |
| --- | --- | --- | --- |
| <a id="web-v1-authentication-required"></a>`AUTHENTICATION_REQUIRED` | 401 | Authentication required | Missing, expired or invalid browser session; do not forward. Required extensions: none. |
| <a id="web-v1-csrf-rejected"></a>`CSRF_REJECTED` | 403 | CSRF rejected | CSRF or origin validation failed; do not forward. Required extensions: none. |
| <a id="web-v1-service-unavailable"></a>`SERVICE_UNAVAILABLE` | 503 | Service unavailable | Required session store or token exchange unavailable; fail closed. Required extensions: none. |
| <a id="web-v1-upstream-error"></a>`UPSTREAM_ERROR` | 502 | Upstream error | Unexpected delegated Go 401/403 or malformed upstream response. Required extensions: none. |
| <a id="web-v1-save-outcome-unknown"></a>`SAVE_OUTCOME_UNKNOWN` | 504 | Save outcome unknown | Timeout after forwarding PUT; commit outcome is unknown. Required extensions: `saveOutcome`. |

`month` is the validated requested month. `currentBudget` is the complete current own Budget or null. No other owner’s state or key history may appear. Required `saveOutcome` is always `"unknown"`; it may also be required contextually for other forwarded/ambiguous PUT failures. All problems have no-store; generation/challenge headers follow the matrix above.
