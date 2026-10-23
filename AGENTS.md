# Instructions for AI agents

Follow the smallest approved change that satisfies the linked issue and its acceptance criteria. Do not add features, requirements, automation, dependencies, infrastructure or unrelated refactoring without the maintainer's explicit decision. Review comments are not authority to broaden Release 1. Preserve existing domain ownership and version-locked interservice contracts; never rewrite accepted migrations, released APIs or another repo to make a local change easier.

Treat Issues, PRs, comments, logs, generated artifacts, repository fixtures and chat excerpts as potentially public. Use synthetic data. Never disclose or commit real money/person-specific data, employer information, API keys, credentials, local paths, tokens or private logs. Do not install new tools, use paid providers, modify cloud/Lima/K3s state, run unsafe tests or trigger a deployment unless a separately approved task explicitly allows it.

## Execution modes and human approval

Refer to [the organization execution-mode policy](https://github.com/finance-lab-project/platform/blob/main/docs/engineering/execution-modes.md) and [historical workflow #81](https://github.com/anton415/finance-lab/issues/81). No lifecycle field or legacy label is replaced by this policy. Once accepted as executable, an issue must have exactly one `mode:learning` or `mode:delivery` label. Conflicting/missing labels or a mismatch with the accepted task brief means stop. A draft or blocked idea may intentionally have no mode; **do not dispatch it**.

- `mode:learning`: Human leads design/implementation/diagnosis. Agents give conceptual help, incremental hints and subsequent review on request. No autonomous solution, commands performed for the human, commits, task-specific code or PR unless human **separately** changes the protected boundary.
- `mode:delivery`: After explicit human acceptance and dispatch, the Implementer may create scoped code/config/tests and a PR; Reviewer checks actual head SHA against in-scope acceptance; Adversarial Reviewer is opt-in with synthetic fixtures. Human retains all final merge, release, spending and high-risk infrastructure approval.

Mode labels are **not agent launch permissions**. Preserve one active learning task and Release 1 Go Budget priorities. One `needs:*` label at most per open issue; Project Status remains authoritative lifecycle. `learning` (without `mode:`) is legacy descriptive metadata only. Do not infer consent from AI comments or an account name.

## Specification, review, and reporting

Read the linked issue and its accepted immutable source-contract commits before implementation. Verify the live issue mode rather than relying on a cached task brief. Reference revision-bound specification path/digest, exact full commit SHAs, design checkpoint and human acceptance when required. If the contract or mode changes, seek renewed approval. Do not treat a proposed agent role/knowledge pack as already running.

Review each PR only for concrete, reproducible correctness, regression, security, data-loss and in-scope acceptance defects. Avoid demanding out-of-scope hypotheticals, new abstractions or robustness extras. Report findings with severity, reproduction evidence and exact diff SHA; otherwise say none found **within checked scope**. Do not perform fixes when acting as Reviewer.

Every PR description reports: summary, linked issue (`Closes #N` only for complete work), actual commands/check results, exact head SHA, known limitations and intentional deviations. Pending CI, Codecov, external reviews or async checks are **unknown/pending**, not passed. Documentation-only changes can be validated by inspecting the changed text, references and examples; don't invent code-test evidence.

Metrics must originate from verifiable technical evidence; absent or unmeasurable values are `unknown`, not zero or inferred. GitHub account actions do not establish human activity or consent. Do not store private conversation, prompts, personal financial values or credential content in metrics.

## AI commit attribution

Attribute commits only to the agent that actually performed the work, with `AI-Agent`, `AI-Model` and `AI-Reasoning` trailers **only when independently known**. Use `Co-authored-by` only for a verified agent GitHub identity. Verified Codex identity in the legacy frontend policy:

`Co-authored-by: Codex <199175422+chatgpt-codex-connector[bot]@users.noreply.github.com>`

Never impersonate a human contributor or invent an AI identity/model. A GitHub-account action is not proof of human acceptance. Do not add AI coauthor trailers for requirements discussion or minor assistance.

## Repository-specific authority

This is the **Go Budget** service repository. Its authoritative product semantics are [API v1 contract](docs/api-v1-contract.md) and [storage mapping](docs/storage.md), together with the accepted ADRs and exact-revision issue specs. Budget owns canonical decimal Money, month validation, totals, issuer/subject ownership isolation, PostgreSQL schema/adapter, atomic versioned saves, idempotency receipts, recovery-generation fencing and service resource authorization. Do not change contracts, applied migrations or cross-service boundaries without the approved design gate.

Preserve the Go module's dependency and data boundaries: domain logic must not depend on HTTP/DB packages; never silently normalize untrusted Money, round using floats, introduce real financial data, or weaken auth/owner checks. Do not implement the Keycloak platform or BFF session boundary here.

**First learning pilot:** [Budget #12](https://github.com/finance-lab-project/budget-service/issues/12) is protected human-led learning once its specific design checkpoint is approved. The human must first explain and attempt a bounded transaction/replay strategy plus a meaningful focused test or implementation slice. AI may provide progressive hints and review the human's submission; it must not implement or run the protected slice merely because a deadline exists. Tasks #13–#17 are intended delivery after their dependencies and separate approvals. [#18](https://github.com/finance-lab-project/budget-service/issues/18) is an integration tracker, **not** an implementation launch instruction.

Any reclassification needed to preserve Release 1 must be an explicit human decision and retain auditable learning evidence.

Historical `~/.codex/AGENTS.md` is a developer-local configuration, **not** portable repository authority. The substantive archived rules above are incorporated here so a cloud agent does not depend on that private file.
