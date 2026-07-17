"""T11 policy tests: deny-wins precedence, glob subdomain match, fail-closed defaults."""

from __future__ import annotations

import addon


def _pol(default, rules):
    doc = {"default": default, "rules": rules}
    return addon.parse_policy(doc)


def test_deny_wins_over_allow():
    pol = _pol(
        "block",
        [
            {"id": "a", "decision": "allow", "resource": "api.example.com"},
            {"id": "d", "decision": "deny", "resource": "api.example.com"},
        ],
    )
    decision, matched = pol.evaluate("api.example.com", 443)
    assert decision == "deny"
    assert matched.id == "d"


def test_allow_when_no_deny_matches():
    pol = _pol("block", [{"id": "a", "decision": "allow", "resource": "api.example.com"}])
    decision, matched = pol.evaluate("api.example.com", 443)
    assert decision == "allow"
    assert matched.id == "a"


def test_block_all_double_star():
    pol = _pol("allow", [{"id": "x", "decision": "deny", "resource": "**"}])
    decision, matched = pol.evaluate("anything.example.org", 443)
    assert decision == "deny"
    assert matched.id == "x"


def test_glob_matches_subdomain_not_apex():
    pol = _pol("block", [{"id": "g", "decision": "allow", "resource": "*.example.com"}])
    assert pol.evaluate("api.example.com", 443)[0] == "allow"
    # Apex must NOT match a "*.example.com" glob.
    assert pol.evaluate("example.com", 443)[0] == "deny"


def test_glob_case_insensitive():
    pol = _pol("block", [{"id": "g", "decision": "allow", "resource": "*.Example.COM"}])
    assert pol.evaluate("API.example.com", 443)[0] == "allow"


def test_default_block_when_missing():
    pol = addon.parse_policy({"rules": []})
    assert pol.default == "deny"
    assert pol.evaluate("anything", 443)[0] == "deny"


def test_empty_default_is_deny():
    pol = _pol("", [])
    assert pol.evaluate("x", 443)[0] == "deny"


def test_malformed_policy_denies_all():
    assert addon.parse_policy("not a dict").evaluate("x", 443)[0] == "deny"
    assert addon.parse_policy(None).evaluate("x", 443)[0] == "deny"
    bad = {"default": "allow", "rules": [{"decision": "allow", "resource": ""}]}
    assert addon.parse_policy(bad).evaluate("x", 443)[0] == "deny"


def test_regex_autodetected_with_metachars():
    pol = _pol("block", [{"id": "r", "decision": "allow", "resource": "^api[0-9]+\\.example\\.com$"}])
    assert pol.evaluate("api42.example.com", 443)[0] == "allow"
    assert pol.evaluate("apix.example.com", 443)[0] == "deny"


def test_star_alone_is_glob_not_regex():
    # A bare '*'/'?' does NOT make a resource a regex.
    pol = _pol("block", [{"id": "g", "decision": "allow", "resource": "*.example.com"}])
    assert pol.evaluate("a.example.com", 443)[0] == "allow"


def test_single_ip_match():
    pol = _pol("block", [{"id": "ip", "decision": "allow", "resource": "192.0.2.10"}])
    assert pol.evaluate("192.0.2.10", 443)[0] == "allow"
    assert pol.evaluate("192.0.2.11", 443)[0] == "deny"


def test_cidr_match_v4_and_v6():
    pol = _pol(
        "block",
        [
            {"id": "c4", "decision": "allow", "resource": "10.0.0.0/8"},
            {"id": "c6", "decision": "allow", "resource": "2001:db8::/32"},
        ],
    )
    assert pol.evaluate("10.1.2.3", 443)[0] == "allow"
    assert pol.evaluate("2001:db8::1", 443)[0] == "allow"
    assert pol.evaluate("11.0.0.1", 443)[0] == "deny"


def test_port_suffix_constrains_match():
    pol = _pol("block", [{"id": "p", "decision": "allow", "resource": "api.example.com:8443"}])
    assert pol.evaluate("api.example.com", 8443)[0] == "allow"
    assert pol.evaluate("api.example.com", 443)[0] == "deny"


def test_denied_flow_gets_403_and_no_injection(tmp_path):
    import json

    from fakes import FakeFlow, FakeRequest

    (tmp_path / "port-registry.json").write_text(json.dumps({"ports": {"51820": {"sandbox": "s1", "state": "active"}}}))
    sb = tmp_path / "sandboxes" / "s1"
    sb.mkdir(parents=True)
    (sb / "policy.yaml").write_text("default: block\nrules: []\n")

    a = addon.PppAddon(data_dir=str(tmp_path), config_dir=str(tmp_path))
    a.reload()
    flow = FakeFlow(51820, FakeRequest("api.anthropic.com", headers={"x-api-key": "client-supplied"}))
    a.request(flow)
    assert flow.response is not None
    assert flow.response.status_code == 403
    # Injection must not have run: the client header is untouched (still present
    # because we never reached the strip/inject path), i.e. no FAKE value.
    assert flow.request.headers["x-api-key"] == "client-supplied"
