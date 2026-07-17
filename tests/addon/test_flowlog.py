"""T11 flow-log lifecycle tests: denied flows logged once, allowed flows on response."""

from __future__ import annotations

import json

import addon
from fakes import FakeFlow, FakeRequest, FakeResponse


def _addon(tmp_path, policy="default: block\nrules: []\n", resolver=None):
    (tmp_path / "port-registry.json").write_text(json.dumps({"51820": "s1"}))
    sb = tmp_path / "sandboxes" / "s1"
    sb.mkdir(parents=True)
    (sb / "policy.yaml").write_text(policy)
    a = addon.PppAddon(
        data_dir=str(tmp_path),
        config_dir=str(tmp_path),
        resolver=resolver or (lambda host: "93.184.216.34"),
    )
    a.reload()
    return a


def _lines(tmp_path):
    text = (tmp_path / "flows.jsonl").read_text()
    return [json.loads(x) for x in text.strip().splitlines() if x]


def test_denied_flow_logged_once(tmp_path):
    a = _addon(tmp_path)  # default block
    flow = FakeFlow(51820, FakeRequest("api.anthropic.com"), response=None)
    a.request(flow)  # sets a 403 synthetic response and logs deny
    a.response(flow)  # mitmproxy fires this for the synthetic response
    records = _lines(tmp_path)
    assert len(records) == 1
    assert records[0]["decision"] == "deny"


def test_allowed_flow_logged_on_response(tmp_path):
    a = _addon(tmp_path, policy="default: allow\nrules: []\n")
    flow = FakeFlow(51820, FakeRequest("unknown.example.com"), response=None)
    a.request(flow)
    # No log yet: allowed flows wait for the response.
    assert not (tmp_path / "flows.jsonl").exists() or _lines(tmp_path) == []
    flow.response = FakeResponse(200, b"ok")
    a.response(flow)
    records = _lines(tmp_path)
    assert len(records) == 1
    assert records[0]["decision"] == "allow"
    assert records[0]["status"] == 200


def test_unknown_sandbox_logged_deny(tmp_path):
    a = _addon(tmp_path)
    flow = FakeFlow(59999, FakeRequest("api.anthropic.com"))
    a.request(flow)
    assert flow.response.status_code == 403
    records = _lines(tmp_path)
    assert len(records) == 1
    assert records[0]["decision"] == "deny"
    assert records[0]["sandbox"] == "unknown"
