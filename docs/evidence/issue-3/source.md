## 1. Problem / evidence

The Go module and pure domain are complete, but Budget has no machine-readable HTTP contract. Freeze the approved v1 behavior before implementing transport, authentication or persistence.

Completed prerequisites:
- [finance-lab#142 — migration specification](https://github.com/anton415/finance-lab/issues/142).
- [finance-lab#143 — Go module](https://github.com/anton415/finance-lab/issues/143), delivered by [budget-service#1](https://github.com/finance-lab-project/budget-service/pull/1).
- [finance-lab#144 — Money, month and exact totals](https://github.com/anton415/finance-lab/issues/144), delivered by [budget-service#2](https://github.com/finance-lab-project/budget-service/pull/2).

Evidence baseline:
- [Stage 0 specification](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/go-budget/stage-0-specification.md).
- [ADR-024: contract-first REST](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/architecture/decisions/0024-openapi-contract-first-rest-versioning.md), [ADR-022: exact Money](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/architecture/decisions/0022-exact-decimal-money-currency-contract.md), [ADR-014: Keycloak service security](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/architecture/decisions/0014-keycloak-service-to-service-security.md), and [ADR-030: implementation sequence](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/architecture/decisions/0030-release1-go-migration-implementation-sequence.md).
- [Implemented domain baseline](https://github.com/finance-lab-project/budget-service/tree/a964c0693fdd475a8a916796fc65f22faa7c0e74/internal/budget).

The approved decisions below resolve the Stage 0 D1–D3 placeholders for this contract. The token profile is approved as the intended interface; actual Keycloak issuance and integration remain unverified.

## 2. Outcome / value

Provide an authoritative Go Budget v1 OpenAPI document, a concise semantic/BFF companion, synthetic examples, and repeatable contract validation. Future Go and BFF work can implement the same owner, Money, error, retry and recovery rules without inventing them separately.

Creating this issue records approved planning. It does not dispatch implementation.

## 3. Behavior

### Routes, representations and exact values

| Operation | Go service | Browser/BFF |
| --- | --- | --- |
| Read | `GET /api/v1/budgets/{month}` | `GET /api/web/v1/budgets/{month}` |
| Full replacement | `PUT /api/v1/budgets/{month}` | `PUT /api/web/v1/budgets/{month}` |

- Month is the complete ASCII `YYYY-MM`: years 0001–9999, months 01–12; no whitespace, trailing text or timezone conversion.
- PUT requires `Content-Type: application/json`, one `Idempotency-Key` and one `Budget-Generation`.
- The body has exactly five required fields: `expectedVersion`, `currency`, `income`, `spending`, `investments`. No owner, month, totals or backup format version.
- `spending` has exactly these 15 required category IDs: `groceries`, `restaurants`, `utilities`, `transport`, `household`, `health`, `personal-care`, `clothing`, `subscriptions`, `education`, `tech`, `culture`, `entertainment`, `gifts`, `travel`.
- Income, investments and each spending entry are either explicit null or a Money object with exactly `amount` and `currency`. Omission is invalid; null means unplanned, while Money amount `"0"` means planned zero.
- New budgets accept only `RUB`, `USD`, `EUR`. Budget currency is immutable. Every populated Money value, including zero, must match it.
- Source amounts are canonical nonnegative ASCII decimal strings: integer part `0` or 1–18 digits beginning with 1–9; optional fraction of 1–6 digits ending in a nonzero digit. Maximum `999999999999999999.999999`. Reject signs, exponent notation, leading integer zeros, trailing fractional zeros and whitespace; never trim, round or normalize invalid inputs.
- `totals.income`, `totals.spending`, `totals.investments`, `totals.remaining` are always Money in the budget currency. Null contributes exact zero. Remaining = income − spending − investments. Totals allow up to 20 integer digits and six fractional digits; only remaining may be negative, with no negative zero. Preserve exact arithmetic and canonical strings.
- `expectedVersion: null` requests creation. Otherwise it is a canonical positive decimal string no greater than `9223372036854775807`. Reject JSON numbers, `"0"`, leading zeros, signs and excess range.
- Create returns 201, version `"1"`, and `Location: /api/v1/budgets/{month}`. Update returns 200 and increments once, including an accepted replacement with unchanged values.
- Successful GET/PUT return the complete Budget: `month`, `version`, `currency`, `income`, `spending`, `investments`, `totals`.
- GET of an absent own month returns 404 and creates neither budget nor owner row.
- Reject missing/unknown fields, wrong types, malformed or trailing JSON, and duplicate decoded keys at every object level, including escaped aliases such as `"currency"` and `"\u0063urrency"`. Raw duplicate detection is required at Go and BFF boundaries before parsing discards evidence.

### D1 — Problem Details and BFF error mapping: approved

Use RFC 9457 `application/problem+json`. Required fields: `type`, `title`, numeric `status` matching the HTTP status, and stable `code`. Titles are fixed English summaries. Optional `detail` is sanitized and never machine-parsed. Producers emit only documented fields; consumers tolerate unknown problem extensions.

Stable type-URI prefix:

`https://github.com/finance-lab-project/budget-service/blob/main/docs/api-v1-contract.md#`

Go anchors use `budget-v1-` plus the lowercase code with underscores replaced by hyphens; BFF-owned anchors use `web-v1-`. Examples: `budget-v1-invalid-request`, `web-v1-csrf-rejected`. Create explicit anchors in the companion document. Keep these v1 identifiers stable even if documentation later moves. Forwarded Go problems retain their original type URI.

| Go status | Code | Meaning |
| --- | --- | --- |
| 400 | `INVALID_REQUEST` | Invalid month/body/Money/revision/key/generation; also attempted currency change after a matching version check. |
| 401 | `AUTHENTICATION_REQUIRED` | Missing, invalid or expired token, including invalid issuer/audience/token profile. |
| 403 | `FORBIDDEN` | Valid identity lacks required permissions or eligible user principal type. |
| 404 | `BUDGET_NOT_FOUND` | No budget for the authenticated owner/month; required `month` extension. |
| 409 | `BUDGET_VERSION_CONFLICT` | Expected absence/version differs; required `currentBudget` is the complete current own Budget, or null if absent. |
| 409 | `IDEMPOTENCY_KEY_REUSED` | Retained exact key has a different validated binding. |
| 409 | `SAVE_RESULT_UNAVAILABLE` | Concurrent duplicate remains unresolved when coordination wait expires; original operation may still commit. |
| 409 | `BUDGET_RECOVERY_REQUIRED` | Well-formed generation differs from current generation. |
| 409 | `BUDGET_VERSION_EXHAUSTED` | Fresh update would exceed the maximum revision. |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | Unsupported request media type. |
| 500 | `INTERNAL_ERROR` | Unexpected internal failure; never invent a successful acknowledgment. |
| 503 | `SERVICE_UNAVAILABLE` | Required dependency unavailable or operator write suspension. |

Require `saveOutcome: "unknown"` for unresolved duplicates and other PUT failures whose commit outcome cannot be established, including forwarded timeouts and ambiguous internal/upstream failures. Its absence is **not** proof that an earlier attempt failed. A current refusal does not describe the complete history of an idempotency key.

Go Bearer challenges:
- Missing credentials: `WWW-Authenticate: Bearer realm="finance-lab-budget"`.
- Invalid/expired token: add `error="invalid_token"`.
- Insufficient scope: use `error="insufficient_scope"` and the required scope list.
- An ineligible principal must not be mislabeled as an insufficient-scope failure. Browser-session authentication errors remain BFF-owned.

| BFF condition | Browser response |
| --- | --- |
| Missing/expired/invalid session | 401 `AUTHENTICATION_REQUIRED`; no forwarding. |
| Failed CSRF/origin check | 403 `CSRF_REJECTED`; no forwarding. |
| Required session store or token exchange unavailable | 503 `SERVICE_UNAVAILABLE`; fail closed. |
| Unexpected delegated Go 401/403, or malformed upstream response | 502 `UPSTREAM_ERROR`. |
| Timeout after forwarding PUT | 504 `SAVE_OUTCOME_UNKNOWN`. |
| Valid documented Go success/problem | Preserve status, body and type; preserve contract headers, except the creation Location rewrite. |

For a forwarded PUT failure, include the unknown-outcome marker unless the outcome is established. Never retry PUT automatically or convert an ambiguous outcome into a definite save failure. UI conflict review applies specifically to `BUDGET_VERSION_CONFLICT`.

### D2 — Processing order, revision limit and recovery: approved

Required order:
1. Authenticate and authorize.
2. Validate request syntax and values.
3. Validate current generation.
4. Resolve owner and inspect a matching retained receipt.
5. For a fresh operation, check expected version, then immutable currency, then revision capacity.
6. Commit budget, version and successful receipt atomically; acknowledge only after commit.

A successful replay at the maximum revision still succeeds without incrementing. A fresh update at `"9223372036854775807"` returns 409 `BUDGET_VERSION_EXHAUSTED` and changes neither budget, version nor successful receipt. GET remains available. Preserve the draft and require operator intervention; never reset the revision or suggest retrying with a new key as a remedy.

| Response | `Budget-Generation` |
| --- | --- |
| GET 200 | Required, current generation |
| Authenticated GET 404 | Required, current generation |
| PUT 200/201, including replay | Required |
| 409 `BUDGET_RECOVERY_REQUIRED` | Required, current generation |
| Other errors | Omitted |

Missing, malformed or repeated request generation is 400. A well-formed mismatch is 409, before owner creation, receipt lookup or mutation.

Generation is persisted outside the restored Budget database. Initialize it once; retain it across ordinary restart and compatible application rollback. Before historical restore is reopened for writes, fence old writers and install a fresh generation. An old generation cannot be repaired by silently resubmitting with the new response header.

React preserves drafts/pending operations, GETs current state, and requires user review before a new operation/key. Historical restoration may lose previously acknowledged writes and receipts within the recovery-point objective; generation does not recover that lost data. Session invalidation, writer fencing and restore evidence belong to later integration/operations work.

### D3 — Header grammar, replay and request binding: approved

The following are complete logical field-value grammars:

| Header | Grammar / meaning |
| --- | --- |
| `Idempotency-Key` | 1–128 ASCII characters from `[A-Za-z0-9._:-]` |
| `Budget-Generation` | 1–128 ASCII characters from `[A-Za-z0-9_-]` |
| `Idempotency-Replayed` | Exactly `true` or `false` |

Header names are case-insensitive; key/generation values are case-sensitive. Reject repeated key/generation headers even when identical, comma-combined values, quotes, embedded whitespace and other characters. HTTP framing's outer optional whitespace is removed by the HTTP parser and is not identity data; validate the parsed field value without further trimming or normalization. Do not claim application-level rejection of framing whitespace already removed by the parser.

Recommend UUIDv4 for newly generated client keys and operator generations; accept every value allowed by the grammars.

- Original successful PUT: `Idempotency-Replayed: false`.
- Successful replay: `Idempotency-Replayed: true`, with original successful status/body and original creation Location where applicable.
- GET and errors omit the replay marker.
- All Budget success and problem responses use `Cache-Control: no-store`.
- BFF preserves key/generation values and response headers; rewrite creation Location to `/api/web/v1/budgets/{month}`. If cross-origin browser access is configured, explicitly allow/expose the relevant contract headers.

Use structured JSON **value equality** for request binding: `apiMajor`, `method`, `month`, `generation`, and complete validated `body`. API major is v1 and method is PUT. Compare exact decoded strings and nulls; ignore object order and insignificant JSON whitespace. Equivalent JSON escapes have the same binding; duplicate decoded keys are rejected first.

Changing month, expected version, amount, currency, null versus zero, or generation changes binding. The generation gate still precedes receipt lookup. Token bytes, expiry and `jti` are excluded; a renewed valid token for the same verified owner can retry. No request-body digest or canonical byte encoding is required.

Scope keys to the verified owner **across months**. Use SHA-256 of the exact accepted ASCII key for indexing, and retain/check the original key and complete binding. A digest collision fails closed with 500 `INTERNAL_ERROR` and no mutation.

A retained matching receipt wins before fresh version/currency checks and returns its historical success. A retained key with a different binding returns `IDEMPOTENCY_KEY_REUSED`. Concurrent duplicates coordinate through database uniqueness; after rollback a waiter may perform normal checks, while after commit it replays. The configurable coordination wait starts at five seconds; this is not an end-to-end request SLA. Unresolved wait returns `SAVE_RESULT_UNAVAILABLE` with unknown outcome, without a second write.

Budget, new version and successful receipt are one transaction; no durable pending receipt. Retain successful receipts for at least 24 hours after commit. Deliberate client retries are allowed only within 24 hours of the operation's first send; retries do not reset that deadline.

Preserve an immutable pending operation separately from later draft edits: key, method/month, generation, exact body, first-send time and identity context. Do not silently retry as another signed-in owner. After the retry deadline, preserve drafts, GET current state and require review before a new operation/key. After receipt cleanup, absence cannot prove that the earlier attempt failed or detect all historical changed-content reuse.

A replay may confirm version `"8"` while current state is `"9"`; confirm the earlier operation and refresh current state without discarding newer edits.

### Token profile — approved contract, integration proof pending

- Exactly one `Authorization: Bearer` header over TLS.
- Accept Keycloak-signed access JWTs, never ID or refresh tokens.
- One explicitly trusted realm issuer per environment, with exact issuer matching. The actual realm URL is deployment configuration, not a fabricated frozen URL.
- Initial algorithm allowlist: RS256 only. Use JWKS configured/discovered from the trusted issuer.
- Sole audience: `finance-lab-budget`, either a string or a one-element array.
- Owner is the exact verified nonempty `(iss, sub)` pair. Never bind by email, request owner fields, client ID or `azp`.
- Require trusted `principal_type: "user"`. Derive this from the subject account during trusted issuance, not an editable user attribute or a mapper that labels every exchanged token as a user. Machine/client-credentials identities cannot impersonate an end user.
- GET requires `budget:read`; PUT requires both `budget:read` and `budget:write`.
- Require valid `iat` and `exp`; lifetime no more than five minutes. Validate `nbf` when present. Allow 30 seconds of clock tolerance.
- Invalid token/signature/algorithm/issuer/audience/lifetime or missing subject yields 401. A valid identity lacking the user principal type or required permissions yields 403.
- Extra claims/scopes grant no additional Budget privileges.
- BFF uses Keycloak standard token exchange V2 to obtain a user-delegated access token with only the Budget audience and required scopes, preserving the end user's subject. BFF owns browser sessions and downstream tokens; React receives no downstream bearer token.
- Verify actual issuance and absence of privilege expansion before integration: requesting a scope is not itself proof of downscoping.
- If a required JWKS lookup is unavailable, fail closed with 503 rather than misclassifying the user token as invalid. An acceptable cached verification key does not require a live Keycloak call.
- Model HTTP bearer JWT security in OpenAPI; document claim/scope requirements per operation. Go exposes no OAuth token endpoint.

## 4. Scope

**In scope**
- `api/openapi.yaml`, using OpenAPI 3.1.
- `docs/api-v1-contract.md`: semantic rules, stable problem anchors/catalog, BFF mapping, approved decisions and future runtime conformance cases.
- Synthetic positive and negative fixtures, clearly classified as schema, raw transport, domain, or state/security cases.
- Repeatable local contract validation and fixture checks with pinned tooling.
- A narrowly scoped GitHub Actions contract check using the same validation entry point.
- A decision/revision record and reviewable verification evidence.

**Out of scope**
- HTTP handlers or runtime JSON decoders; authentication middleware; persistence adapters or migrations.
- Keycloak deployment/configuration; BFF or React implementation; SDK generation or deployment.
- New endpoints/features, legacy data import, changes to existing Money/domain behavior or legacy V2/Spring issues, and unrelated CI redesign.

## 5. Contracts / dependencies

| Owner | Responsibility |
| --- | --- |
| Go Budget | Authoritative service schema and domain/error semantics; future token verification, owner isolation, exact totals, atomic revision and replay behavior. |
| Keycloak / platform | Trusted user identity and access-token issuance; exchange/downscoping, realm/JWKS configuration and operational availability. |
| BFF | Browser contract, session/CSRF/origin validation, delegated tokens, strict boundary validation and exact Go/browser mapping. |
| React | Preserve strings/nulls, drafts and pending operations; explicit conflict/recovery/retry decisions; no authoritative financial arithmetic or bearer-token ownership. |

The companion documents the BFF mapping without transferring ownership of the browser contract to Go.

Cross-repository planning reference: [Go Budget — First authenticated monthly budget slice](https://github.com/anton415/finance-lab/milestone/11). This is a planning link, not assignment of that other repository's milestone to this issue.

Keycloak, persistence, browser and recovery integration are later work. Their unavailable runtime evidence must not block authoring this contract or be reported as already verified.

## 6. Data / security invariants

- All private data and receipts are isolated by verified exact issuer/subject. Never reveal another owner's budget, key history or conflict snapshot.
- Authenticate/authorize every request, including replay, before private-data access.
- Exact decimal/revision strings and explicit nulls survive every boundary; no authoritative float conversion.
- Generation refusal precedes owner creation, receipt lookup and mutation.
- Failed fresh-operation checks change neither budget/version nor successful receipt; an ambiguous failure is not a rollback guarantee.
- A successful acknowledgment requires committed budget/version/receipt. Receipt history does not survive historical restore beyond the stated recovery guarantee.
- Use synthetic examples only. Problems, fixtures and evidence expose no real tokens, secrets or private budgets.

## 7. Acceptance / negative cases

- [ ] **AC01 — Decisions:** Record approved D1, D2, D3 and the token profile, including their alternatives/tradeoffs. No undecided type base, exhaustion behavior or header/binding placeholder is represented as frozen. Clearly separate the approved profile from pending Keycloak proof.
- [ ] **AC02 — OpenAPI:** OpenAPI 3.1 validates with resolved references and unique operation IDs. Both GET/PUT have complete parameters, request/response schemas, security requirements, success/error variants and the specified response-header matrix.
- [ ] **AC03 — Exact representations:** Examples and checks cover all three currencies, null versus zero, minimum/maximum amounts, canonical signs/scales, exact `0.1 + 0.2 = 0.3`, aggregates beyond 18 integer digits, negative remaining, revision strings and their range.
- [ ] **AC04 — Negative inputs:** Cover wrong JSON types, missing/unknown fields/categories, invalid months, currency mismatch, noncanonical amounts/revisions, bad/duplicate headers, malformed/trailing JSON and duplicate decoded keys including escaped aliases at every object level.
- [ ] **AC05 — Validation limits:** Label every fixture/rule by what schema/tooling can prove and what needs future raw-transport, domain or state/security tests. Do not claim schema validation proves duplicate-key rejection, cross-field equality, token verification or transactions.
- [ ] **AC06 — Save semantics:** Document create, update, create-again conflict, stale version, attempted currency change and maximum revision traces; rejected fresh operations leave budget/version/success receipt unchanged. Retained successful replay remains valid at the revision limit.
- [ ] **AC07 — Idempotency:** Cover equivalent reordered/escaped JSON, identical replay, changed binding, reuse across months, independent owners, reauthentication as the same owner, concurrent unresolved duplicates, historical replay older than current state, receipt expiry and the first-send retry deadline.
- [ ] **AC08 — Recovery:** Check the full generation header/status matrix and processing order. Generation mismatch precedes owner creation/replay/mutation. Explain ordinary restart versus historical restore, writer fencing, preserved drafts and the acknowledged-write-loss exception.
- [ ] **AC09 — Identity/profile:** Specify valid delegated users, invalid tokens, insufficient permissions and ineligible machine identities; distinguish 401/403/503, Bearer challenges, exact owner binding and per-operation scopes. List actual Keycloak issuance/downscoping tests as pending integration evidence.
- [ ] **AC10 — Problems/BFF:** Every Go/BFF code has a stable type anchor, fixed title, matching status, documented extensions and outcome meaning. Cover Location rewrite, generation/replay/cache headers, browser authentication/CSRF handling, unknown forwarded-write outcomes and no automatic PUT retry.
- [ ] **AC11 — Verification:** Pin validator/fixture-tool versions; document one reproducible local entry point reused by the scoped CI check. Record actual commands/results, positive passes and negative failures for the intended reasons. Mark unavailable runtime checks explicitly NOT RUN.
- [ ] **AC12 — Acceptance/scope:** The maintainer accepts the exact produced contract revision after review. Evidence identifies what was validated and what remains future work. No runtime/infrastructure/domain/legacy implementation is included.

## 8. Verification / evidence

When this issue is separately authorized for implementation:
1. Select and pin an OpenAPI 3.1 validator and compatible fixture validation approach; document their limitations and local entry point.
2. Validate syntax, references, operation IDs and schema examples. Check positive fixtures pass and each schema-negative fixture fails for its intended reason.
3. Review raw transport and state/security cases as explicit future conformance requirements; retain raw duplicate-key fixtures without preprocessing that erases the duplicates.
4. Cross-check schemas, examples, domain invariants, error catalog, BFF mapping, response headers and the approved decision record.
5. Run the same contract checks locally and in the scoped CI job; record observed commands, versions, exit results and remaining gaps.
6. Obtain human acceptance tied to the exact delivered contract revision. Do not invent a compatibility diff against a released Go API: this is the initial baseline.

Live authentication, ownership isolation, database concurrency/atomicity, restart/retention behavior and restore fencing remain later runtime verification. Passing OpenAPI checks is not evidence that those behaviors work.

## 9. Learning / human demonstration

The maintainer can trace creation, conflict, replay after lost confirmation, and historical-restore rejection; explain null versus zero, source amounts versus totals, valid identity versus authorized ownership, and the limits of schema validation. Record the demonstration outcome when reviewing the delivered contract; do not pre-mark it complete.

## 10. Design checkpoint

This is an interface/identity/retry/recovery design checkpoint. Extend the Stage 0 reasoning with a short record of the approved choices and alternatives:
- Repository-hosted stable problem identifiers instead of waiting for a documentation domain.
- External recovery generation plus revision exhaustion refusal; no revision reset.
- Structured validated-value equality instead of a raw-body hash/canonical byte format.
- Narrow delegated user-token profile with actual Keycloak evidence deferred to integration.

The service OpenAPI is authoritative for Go wire shapes; the companion explains invariants and browser mapping. Resolve any material inconsistency before accepting the contract rather than silently changing domain behavior.

## 11. Questions / risks

No D1–D3 selection remains open for contract authoring. Track these separately:
- **D4:** Platform and Go/BFF security owners must verify the actual Keycloak version, realm/client configuration, trusted principal claim, sole audience, token lifetime and effective scope downscoping before authenticated integration.
- **D5 storage:** Go/platform owners select PostgreSQL/Goose versions and prove schema compatibility during persistence work; repository/module/toolchain and domain placement are already resolved.
- **D6–D8:** BFF/platform/React owners implement and verify session lifecycle, token protection, refresh/revocation, measured timeouts, receipt cleanup, external generation distribution, writer fencing and draft preservation during their respective integration stages.
- GitHub-hosted type URIs are deliberately stable identifiers. Their future documentation relocation must preserve the existing v1 identifiers.
- Tooling cannot express or execute every semantic invariant; the conformance classification and explicit NOT RUN evidence are required to prevent false assurance.

These are integration gates or documented limitations, not a request to broaden this issue.

## 12. Revision / decisions

- Revision 2, 2026-10-20; first GitHub publication of this issue.
- Author: Codex (OpenAI), incorporating the maintainer's approved planning decisions.
- Human decision: the maintainer approved the concrete D1–D3/token recommendations and explicitly requested creation of this issue. Implementation remains explicitly excluded from the current authorization.
- The future OpenAPI artifact, independent review where applicable, human demonstration and exact-revision acceptance are not yet complete. No independent PASS, artifact digest or runtime verification is claimed.
- Review structure follows the [Finance Lab specification contract](https://github.com/anton415/finance-lab/blob/6c7e39758fda11c2a9679feba6cc2d32e14182e3/docs/agentops/specification-contract.md); publication does not imply automated Project promotion.

Decision basis: [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457.html), [RFC 6750 Bearer challenges](https://www.rfc-editor.org/rfc/rfc6750.html#section-3), [RFC 9110 field values](https://www.rfc-editor.org/rfc/rfc9110.html#section-5.5), [RFC 8725 JWT algorithm verification](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.1), [Keycloak standard token exchange](https://www.keycloak.org/securing-apps/token-exchange#_standard-token-exchange), and [OpenAPI 3.1](https://spec.openapis.org/oas/v3.1.1.html).
