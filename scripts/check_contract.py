#!/usr/bin/env python3
"""Offline contract/fixture checks. This is not a Go/BFF runtime conformance test."""

import hashlib
import importlib.metadata
import json
import platform
import re
import sys
from collections import Counter
from pathlib import Path

import yaml
from jsonschema import Draft202012Validator, FormatChecker
from openapi_spec_validator import OpenAPIV31SpecValidator

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "fixtures/api-v1"
PATH = "/api/v1/budgets/{month}"
TYPE_BASE = (
    "https://github.com/finance-lab-project/budget-service/"
    "blob/main/docs/api-v1-contract.md#"
)


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, f"Duplicate key in authored fixture/tooling input: {key}")
        result[key] = value
    return result


class UniqueYamlLoader(yaml.SafeLoader):
    pass


def yaml_mapping(loader, node):
    return unique_object(
        (loader.construct_object(key), loader.construct_object(value))
        for key, value in node.value
    )


UniqueYamlLoader.add_constructor(
    yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, yaml_mapping
)


def read_cases(filename, classification):
    cases = json.loads(
        (FIXTURES / filename).read_text(), object_pairs_hook=unique_object
    )
    require(cases, f"Empty corpus: {filename}")
    ids = [case["id"] for case in cases]
    require(len(ids) == len(set(ids)), f"Duplicate case ID in {filename}")
    for case in cases:
        require(case["class"] == classification, f"{case['id']}: wrong classification")
        require(
            case["ac"] and all(re.fullmatch(r"AC(?:0[1-9]|1[0-2])", ac) for ac in case["ac"]),
            f"{case['id']}: missing/invalid acceptance mapping",
        )
    return cases


def walk(value):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)


def dereference(spec, value):
    if "$ref" not in value:
        return value
    reference = value["$ref"]
    require(reference.startswith("#/"), f"Unexpected nonlocal reference: {reference}")
    target = spec
    for segment in reference[2:].split("/"):
        target = target[segment.replace("~1", "/").replace("~0", "~")]
    return target


def validator(spec, schema):
    # Keep the OpenAPI components in the reference root; never copy/relocate a
    # subschema and accidentally break its #/components/... references.
    return Draft202012Validator(
        {
            "$schema": "https://json-schema.org/draft/2020-12/schema",
            "components": spec["components"],
            **schema,
        },
        format_checker=FormatChecker(),
    )


def schema_ref(name):
    return {"$ref": "#/components/schemas/" + name}


def all_errors(errors):
    for error in errors:
        yield error
        yield from all_errors(error.context)


def check_schema_cases(spec):
    cases = read_cases("schema-cases.json", "schema")
    for case in cases:
        errors = list(validator(spec, schema_ref(case["schema"])).iter_errors(case["instance"]))
        if case["valid"]:
            require(not errors, f"{case['id']}: unexpected schema rejection: {errors}")
        else:
            expected = case["expectedError"]
            require(
                any(
                    error.validator == expected["keyword"]
                    and list(error.absolute_path) == expected["path"]
                    for error in all_errors(errors)
                ),
                f"{case['id']}: did not reject at intended keyword/path {expected}",
            )
    counts = Counter(case["valid"] for case in cases)
    print(f"PASS schema fixtures: {counts[True]} positive; {counts[False]} intended negative")
    return cases


def scaled(amount):
    """Exact integer millionths for checking example arithmetic only."""
    negative = amount.startswith("-")
    integer, _, fraction = amount.lstrip("-").partition(".")
    units = int(integer) * 1_000_000 + int(fraction.ljust(6, "0") or "0")
    return -units if negative else units


def example_errors(budget):
    populated = [
        item
        for item in [
            budget["income"], budget["investments"],
            *budget["spending"].values(), *budget["totals"].values(),
        ]
        if item is not None
    ]
    if any(item["currency"] != budget["currency"] for item in populated):
        return {"currency-mismatch"}

    def units(item):
        return 0 if item is None else scaled(item["amount"])

    income = units(budget["income"])
    spending = sum(units(item) for item in budget["spending"].values())
    investments = units(budget["investments"])
    expected = {
        "income": income, "spending": spending, "investments": investments,
        "remaining": income - spending - investments,
    }
    return (
        {"totals-mismatch"}
        if any(units(budget["totals"][key]) != value for key, value in expected.items())
        else set()
    )


def check_domain_examples(spec):
    cases = read_cases("domain-cases.json", "domain")
    for case in cases:
        validator(spec, schema_ref("Budget")).validate(case["budget"])
        errors = example_errors(case["budget"])
        require(
            not errors if case["valid"] else errors == {case["expectedError"]},
            f"{case['id']}: wrong example consistency result: {errors}",
        )
    counts = Counter(case["valid"] for case in cases)
    print(f"PASS exact example consistency: {counts[True]} positive; {counts[False]} intended negative")
    return cases


def response_errors(spec, case):
    headers = {name.lower(): value for name, value in case["headers"].items()}
    status, method, body = case["status"], case["method"], case["body"]
    code = body.get("code")
    definition = spec["paths"][PATH][method]["responses"][str(status)]
    expected_media = "application/json" if status in (200, 201) else "application/problem+json"
    errors = set()
    if headers.get("content-type") != expected_media:
        errors.add("media")
    if list(validator(spec, definition["content"][expected_media]["schema"]).iter_errors(body)):
        errors.add("body")
    if status >= 400 and body["status"] != status:
        errors.add("status")
    if headers.get("cache-control") != "no-store":
        errors.add("cache")
    needs_generation = status in (200, 201) or (method == "get" and status == 404) or code == "BUDGET_RECOVERY_REQUIRED"
    if needs_generation:
        if not validator(spec, schema_ref("Generation")).is_valid(headers.get("budget-generation")):
            errors.add("generation")
    elif "budget-generation" in headers:
        errors.add("generation")
    if method == "put" and status in (200, 201):
        if headers.get("idempotency-replayed") not in ("true", "false"):
            errors.add("replay")
    elif "idempotency-replayed" in headers:
        errors.add("replay")
    if status == 201:
        if headers.get("location") != "/api/v1/budgets/" + body["month"]:
            errors.add("location")
    elif "location" in headers:
        errors.add("location")
    if method == "get" and "saveOutcome" in body:
        errors.add("outcome")
    for name, declaration in definition["headers"].items():
        declaration = dereference(spec, declaration)
        value = headers.get(name.lower())
        # Matrix-specific diagnostics above give negative fixtures stable reasons.
        if name == "WWW-Authenticate":
            if declaration.get("required") and value is None:
                errors.add("challenge")
            if value is not None and not validator(spec, declaration["schema"]).is_valid(value):
                errors.add("challenge")
    if status == 403 and "insufficient_scope" in headers.get("www-authenticate", ""):
        scopes = " ".join(spec["paths"][PATH][method]["x-required-scopes"])
        expected = f'Bearer realm="finance-lab-budget", error="insufficient_scope", scope="{scopes}"'
        if headers["www-authenticate"] != expected:
            errors.add("challenge")
    return errors


def check_responses(spec):
    cases = read_cases("response-cases.json", "schema")
    for case in cases:
        errors = response_errors(spec, case)
        require(
            not errors if case["valid"] else errors == {case["expectedError"]},
            f"{case['id']}: wrong response consistency result: {errors}",
        )
    for method in ("get", "put"):
        expected_statuses = (
            {"200", "400", "401", "403", "404", "500", "503"}
            if method == "get"
            else {"200", "201", "400", "401", "403", "409", "415", "500", "503"}
        )
        require(set(spec["paths"][PATH][method]["responses"]) == expected_statuses, f"Wrong {method} statuses")
        for status, definition in spec["paths"][PATH][method]["responses"].items():
            samples = [c for c in cases if c["valid"] and c["method"] == method and c["status"] == int(status)]
            require(samples, f"No response sample for {method} {status}")
            expected_headers = {"Cache-Control": True}
            if status in ("200", "201", "404"):
                expected_headers["Budget-Generation"] = True
            if method == "put" and status in ("200", "201"):
                expected_headers["Idempotency-Replayed"] = True
            if status == "201":
                expected_headers["Location"] = True
            if status == "409":
                expected_headers["Budget-Generation"] = False
            if status in ("401", "403"):
                expected_headers["WWW-Authenticate"] = status == "401"
            require(set(definition["headers"]) == set(expected_headers), f"Wrong {method} {status} header declarations")
            for name, header in definition["headers"].items():
                declaration = dereference(spec, header)
                required = declaration.get("required", False)
                require(required == expected_headers[name], f"Wrong required flag: {method} {status} {name}")
                require(not required or all(name in c["headers"] for c in samples), f"Required header mismatch: {method} {status} {name}")
                for sample in samples:
                    if name in sample["headers"]:
                        validator(spec, declaration["schema"]).validate(sample["headers"][name])
            if status == "409":
                header = definition["headers"]["Budget-Generation"]
                require(not header.get("required"), "Generation must not be required on all 409 responses")
                require(header["x-required-for-code"] == "BUDGET_RECOVERY_REQUIRED", "Wrong conditional generation code")
    counts = Counter(c["valid"] for c in cases)
    print(f"PASS response matrix: {counts[True]} positive; {counts[False]} intended negative")
    return cases


def check_catalog(spec, companion):
    catalog = json.loads((FIXTURES / "problems.json").read_text())
    for problem in catalog:
        require(problem["ac"] == ["AC10"], "Missing problem acceptance mapping")
        require(problem["class"] == ("schema" if problem["owner"] == "budget" else "state/security"), "Wrong problem classification")
        example, anchor = problem["example"], problem["anchor"]
        require(f'<a id="{anchor}"></a>' in companion, f"Missing explicit problem anchor: {anchor}")
        require(example["type"] == TYPE_BASE + anchor, f"Wrong problem URI: {anchor}")
        require(example["title"] == problem["title"] and example["status"] == problem["status"], f"Problem catalog mismatch: {anchor}")
        require(f'| {problem["status"]} | {problem["title"]} |' in companion, f"Title/status missing from companion: {anchor}")
        if problem["owner"] == "budget":
            validator(spec, schema_ref(problem["schema"])).validate(example)
        else:
            require(problem["schema"] is None, "BFF contract must not become a Go operation/schema")
    print(f"PASS problem catalog: {len(catalog)} stable Go/BFF anchors and examples")


def check_future_cases():
    raw = read_cases("raw-cases.json", "raw transport")
    for case in raw:
        data = (FIXTURES / case["path"]).read_bytes()
        require(hashlib.sha256(data).hexdigest() == case["sha256"], f"Raw bytes changed: {case['id']}")
        require(case["verification"].startswith("NOT RUN") and case["precondition"] and case["expected"], f"Incomplete raw case: {case['id']}")
    state = read_cases("state-cases.json", "state/security")
    for case in state:
        require(all(case[key] for key in ("given", "when", "then")), f"Incomplete trace: {case['id']}")
        require(case["verification"].startswith("NOT RUN"), f"Unexecuted runtime claim: {case['id']}")
    print(f"PASS inventory/integrity only: {len(raw)} raw cases; {len(state)} state/security cases")
    print("NOT RUN: Go/BFF transport, identity, persistence, concurrency, retention, restart and recovery conformance")
    return raw + state


def main():
    manifest = ROOT / "docs/evidence/issue-3/SHA256SUMS"
    for line in manifest.read_text().splitlines():
        digest, path = line.split("  ", 1)
        require(hashlib.sha256((ROOT / path).read_bytes()).hexdigest() == digest, f"Contract revision digest mismatch: {path}")
    require(platform.python_version() == (ROOT / ".python-version").read_text().strip(), "Use the pinned Python version from .python-version")
    for line in (ROOT / "requirements-contract.txt").read_text().splitlines():
        if line and not line.startswith("#"):
            package, version = line.split("==")
            require(importlib.metadata.version(package) == version, f"Dependency pin mismatch: {package}")
    spec = yaml.load((ROOT / "api/openapi.yaml").read_text(), Loader=UniqueYamlLoader)
    require(spec["openapi"] == "3.1.1", "Unexpected OpenAPI version")
    OpenAPIV31SpecValidator(spec).validate()
    for node in walk(spec):
        if "$ref" in node:
            dereference(spec, node)
    require(set(spec["paths"]) == {PATH}, "Unexpected endpoint")
    operations = [spec["paths"][PATH][method] for method in ("get", "put")]
    ids = [operation["operationId"] for operation in operations]
    require(len(set(ids)) == len(ids), "Duplicate operation IDs")
    for operation, scopes in zip(operations, (["budget:read"], ["budget:read", "budget:write"])):
        require(operation["security"] == [{"BearerAuth": []}], "Wrong bearer requirement")
        require(operation["x-required-scopes"] == scopes, "Wrong operation scope list")
    domain_source = (ROOT / "internal/budget/totals.go").read_text()
    category_block = re.search(r"return \[15\]string\{(.*?)\}", domain_source, re.S).group(1)
    categories = re.findall(r'"([^"]+)"', category_block)
    spending = spec["components"]["schemas"]["Spending"]
    require(spending["required"] == categories and list(spending["properties"]) == categories, "Domain/category contract drift")
    examples = 0
    for node in walk(spec):
        if "schema" in node and isinstance(node.get("examples"), dict):
            for example in node["examples"].values():
                validator(spec, node["schema"]).validate(example["value"])
                examples += 1
        elif isinstance(node.get("examples"), list):
            for example in node["examples"]:
                validator(spec, node).validate(example)
                examples += 1
    print(f"PASS OpenAPI 3.1.1: references, operation IDs, security, category parity; {examples} embedded examples")
    cases = check_schema_cases(spec)
    cases += check_domain_examples(spec)
    cases += check_responses(spec)
    check_catalog(spec, (ROOT / "docs/api-v1-contract.md").read_text())
    cases += check_future_cases()
    coverage = {ac for case in cases for ac in case["ac"]}
    require({f"AC{i:02}" for i in range(2, 11)} <= coverage, "Missing fixture coverage for AC02–AC10")
    print("PASS contract checks; runtime behavior and human acceptance are not established")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, AssertionError) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
