# Issue #3: contract revision 1 verification

## Revision and authority

- Target: [finance-lab-project/budget-service #3](https://github.com/finance-lab-project/budget-service/issues/3).
- Author: Codex. This is implementer verification, not an independent review.
- Issue baseline: revision 2, updated `2026-10-20T17:31:51Z`.
- [Captured issue](source.md): UTF-8 body normalized to exactly one final LF.
  SHA-256: `c7aedd619739f6180fa8313be7cc342b3d4d4a3f7f882df016c27189ad0cbcf0`.
- Source specification/ADRs: `anton415/finance-lab@6c7e39758fda11c2a9679feba6cc2d32e14182e3`.
- Go domain baseline: `finance-lab-project/budget-service@a964c0693fdd475a8a916796fc65f22faa7c0e74`.
- Design record: revision 1, [approved choices and tradeoffs](../../api-v1-contract.md#baseline-decisions-and-ownership).
- Delivered artifact set: [SHA256SUMS](SHA256SUMS), using repository-relative paths.
  It covers the contract, companion, fixtures, checker, dependency/runtime pins,
  workflow, README and captured issue. It excludes itself and this evidence file.
  The checker verifies those bytes before validation.
- Human acceptance and demonstration: **PENDING**, tied to the final reviewed
  commit, artifact manifest digest, captured-issue digest and source revisions.
  The instruction to implement is not acceptance of the produced artifact.
- Independent review: **NOT RUN**. No independent PASS is claimed.
- This is the initial baseline; there is no released Go contract to diff against.

Use the full reviewed commit when linking these files for acceptance. Any change
to contract artifacts requires regenerating the manifest and renewed review/
acceptance; do not inherit acceptance from a previous digest.

## Reproduce

Use Python **3.14.5**, as pinned in `.python-version`. From the repository root:

```sh
python3.14 -m venv .venv
. .venv/bin/activate
python -m pip install -r requirements-contract.txt
python scripts/check_contract.py
git diff --check
```

The core validators are `openapi-spec-validator==0.9.0`,
`jsonschema==4.26.0`, and `PyYAML==6.0.3`; all installed transitive
dependencies are also pinned in `requirements-contract.txt`. The checker verifies
installed versions. Installation requires package access; validation uses local
references and needs no service, database, token or network access.

The scoped GitHub Actions job uses the same runtime/dependency pins and
`python scripts/check_contract.py`. The action implementations are pinned to
resolved official commit SHAs. No Go runtime dependency is added.

## Observed verification

Local validation used Python 3.14.5 and the pinned dependency set. Observed:

| Command / inspection | Result |
| --- | --- |
| `python scripts/check_contract.py` | Exit 0: OpenAPI 3.1.1, all local references, unique operation IDs, security/scope declarations, domain-category parity and 35 embedded examples passed |
| Same checker: schema fixtures | 63 positive passes; 183 negatives rejected at their intended keyword and instance path |
| Same checker: exact example consistency | 6 positive passes; 5 intentional currency/total mismatches detected |
| Same checker: response matrix | 27 positive passes; 12 negatives rejected for their intended header/media/challenge rule |
| Same checker: problem catalog | 17 Go/BFF anchors, fixed titles/statuses and examples checked |
| Same checker: future conformance inventory | 19 raw byte digests and 41 state/security case descriptions checked; runtime execution NOT RUN |
| `python -m pip check` | Exit 0: no broken requirements |
| Authoring inspection using Python `yaml.BaseLoader`, `json.loads` with duplicate-preserving hooks, and local file-link resolution | Workflow parsed with scoped events, read-only permission, pinned action SHAs and the shared entry point; all 19 raw samples contained the intended defect; local documentation links resolved |
| `git diff --cached --check` | Exit 0 after explicitly preserving the raw HTTP fixtures' intentional CRLF/empty-body framing |
| `git diff --cached --exit-code -- go.mod internal` | Exit 0: Go module and domain implementation unchanged |

Raw-fixture authoring inspection validates the test inputs, not the future
Go/BFF decoders. Fixture arithmetic validates the examples, not an executing API.
GitHub Actions results belong to the linked PR's authoritative check run; this
local evidence does not predict them. No Go domain tests were rerun for this
contract-only change. Live HTTP, auth, storage, concurrency and restore tests
remain NOT RUN.

## Acceptance map and limits

| Criterion | Artifact / evidence | Limit |
| --- | --- | --- |
| AC01 | Companion decision table and source/revision record | Intended token interface approved; real issuance unverified |
| AC02 | OpenAPI, embedded examples, response fixtures and checker | No live HTTP conformance |
| AC03 | Schema and domain-example corpus: all currencies, null/zero, bounds, exact tenths, 20-digit totals, signed remaining and revision ceiling | Example arithmetic is not service execution |
| AC04 | Negative schema fixtures, currency examples and preserved raw corpus | Raw Go/BFF boundary rejection NOT RUN |
| AC05 | Fixture classifications, README and explicit checker output | No schema claim for transactions, JWTs or duplicates |
| AC06 | Save/order/exhaustion companion and concrete state traces | Storage, rollback and revision runtime tests NOT RUN |
| AC07 | Binding/replay/retention companion and state traces | Receipt concurrency, cleanup, owner isolation and retry UI NOT RUN |
| AC08 | Header matrix fixtures, processing order and restart/restore traces | External generation distribution/fencing/restore NOT RUN |
| AC09 | Token profile, challenges and identity/JWKS/issuance traces | Keycloak/delegation/downscoping integration NOT RUN |
| AC10 | 17 Go/BFF stable problem anchors, catalog, response matrix and BFF traces | Outcome knowledge, browser boundary and consumer behavior NOT RUN |
| AC11 | Locked tooling, single checker, scoped workflow and observed results | Only completed and inspected checks may be reported as passed |
| AC12 | Reviewable revision/digests; domain, module and legacy implementations unchanged | Exact-revision human acceptance remains PENDING |

## Human demonstration checklist — not yet performed

During review, trace creation, stale-version conflict, replay after lost
confirmation and historical-restore rejection. Explain:

- null versus planned zero and source limits versus total limits;
- exact identity versus permission and own-budget isolation;
- retry deadline versus receipt retention, preserving later draft edits;
- unknown save outcome and the acknowledged-write-loss exception on restore;
- what schema checks prove and what needs raw transport/state/security tests.

Record the maintainer's actual outcome and exact accepted revision. Do not mark
this checklist complete from tool output.
