"""T11 policy-load tests: per-sandbox + global merge, fail-closed on disk errors."""

from __future__ import annotations

import addon


def _write(tmp_path, sandbox, sandbox_yaml=None, global_yaml=None):
    if sandbox_yaml is not None:
        sb = tmp_path / "sandboxes" / sandbox
        sb.mkdir(parents=True, exist_ok=True)
        (sb / "policy.yaml").write_text(sandbox_yaml)
    if global_yaml is not None:
        (tmp_path / "policies.yaml").write_text(global_yaml)
    return str(tmp_path), str(tmp_path / "policies.yaml")


def test_missing_both_denies(tmp_path):
    data_dir, gp = _write(tmp_path, "s1")
    pol = addon.load_policy_for("s1", data_dir, gp)
    assert pol.evaluate("api.anthropic.com", 443)[0] == "deny"


def test_sandbox_rules_evaluated_before_global(tmp_path):
    data_dir, gp = _write(
        tmp_path,
        "s1",
        sandbox_yaml="default: block\nrules:\n  - id: sb\n    decision: deny\n    resource: api.anthropic.com\n",
        global_yaml="default: allow\nrules:\n  - id: g\n    decision: allow\n    resource: api.anthropic.com\n",
    )
    pol = addon.load_policy_for("s1", data_dir, gp)
    # Deny wins regardless of ordering; sandbox deny beats global allow.
    decision, matched = pol.evaluate("api.anthropic.com", 443)
    assert decision == "deny"
    assert matched.id == "sb"


def test_sandbox_default_overrides_global_default(tmp_path):
    data_dir, gp = _write(
        tmp_path,
        "s1",
        sandbox_yaml="default: allow\nrules: []\n",
        global_yaml="default: block\nrules: []\n",
    )
    pol = addon.load_policy_for("s1", data_dir, gp)
    assert pol.evaluate("unmatched.example", 443)[0] == "allow"


def test_global_default_used_when_sandbox_absent(tmp_path):
    data_dir, gp = _write(tmp_path, "s1", global_yaml="default: allow\nrules: []\n")
    pol = addon.load_policy_for("s1", data_dir, gp)
    assert pol.evaluate("x.example", 443)[0] == "allow"


def test_malformed_sandbox_yaml_fails_closed(tmp_path):
    data_dir, gp = _write(
        tmp_path,
        "s1",
        sandbox_yaml="default: allow\nrules:\n  - decision: bogus\n    resource: x\n",
        global_yaml="default: allow\nrules: []\n",
    )
    pol = addon.load_policy_for("s1", data_dir, gp)
    # An unparseable sandbox rule denies the whole sandbox policy; the merged
    # policy therefore has no allow rules and a deny default.
    assert pol.evaluate("x.example", 443)[0] == "deny"


def test_unparseable_sandbox_file_denies_not_global_fallback(tmp_path):
    # A per-sandbox policy that exists but cannot be PARSED at the file level
    # (invalid YAML) must fail closed for that sandbox, NOT silently fall back
    # to a permissive global policy (code review S4).
    data_dir, gp = _write(
        tmp_path,
        "s1",
        sandbox_yaml="default: allow\nrules: [this is : not : valid yaml\n",
        global_yaml="default: allow\nrules: []\n",
    )
    pol = addon.load_policy_for("s1", data_dir, gp)
    assert pol.evaluate("x.example", 443)[0] == "deny"


def test_unparseable_global_file_fails_closed(tmp_path):
    data_dir, gp = _write(
        tmp_path,
        "s1",
        sandbox_yaml="default: allow\nrules: []\n",
        global_yaml="default: allow\nrules: [broken : : :\n",
    )
    pol = addon.load_policy_for("s1", data_dir, gp)
    assert pol.evaluate("x.example", 443)[0] == "deny"


def test_regex_resource_is_case_insensitive(tmp_path):
    # Parity with the Go engine: regex host matching is case-insensitive.
    data_dir, gp = _write(
        tmp_path,
        "s1",
        sandbox_yaml=(
            "default: block\nrules:\n"
            "  - id: rx\n    decision: allow\n    resource: '^API[0-9]+\\.example\\.com$'\n"
        ),
    )
    pol = addon.load_policy_for("s1", data_dir, gp)
    assert pol.evaluate("api42.example.com", 443)[0] == "allow"
