"""T11 redaction tests: flows.jsonl carries no secret/header values."""

from __future__ import annotations

import json

import addon
from fakes import FakeFlow, FakeRequest, FakeResponse, FakeUdsServer


def test_flow_log_has_fields_and_no_secret(tmp_path):
    (tmp_path / "port-registry.json").write_text(json.dumps({"ports": {"51820": {"sandbox": "ppp-red-bird", "state": "active"}}}))
    sb = tmp_path / "sandboxes" / "ppp-red-bird"
    sb.mkdir(parents=True)
    (sb / "policy.yaml").write_text("default: allow\nrules: []\n")

    def handler(req):
        if req.get("service") == "anthropic":
            return {"ok": True, "header": "x-api-key", "value": "FAKE-KEY-123"}
        return {"ok": True, "substitutions": []}

    server = FakeUdsServer(str(tmp_path / "secret.sock"), handler)
    a = addon.PppAddon(
        data_dir=str(tmp_path),
        config_dir=str(tmp_path),
        resolver=lambda host: "93.184.216.34",
    )
    a.reload()
    try:
        req = FakeRequest(
            "api.anthropic.com",
            method="POST",
            headers={"x-api-key": "client-supplied", "Authorization": "Bearer secret-token"},
        )
        flow = FakeFlow(51820, req, response=FakeResponse(200, b"ok"))
        a.request(flow)
        a.response(flow)
    finally:
        server.close()

    log_text = (tmp_path / "flows.jsonl").read_text()
    line = log_text.strip().splitlines()[-1]
    record = json.loads(line)
    for field in ("sandbox", "timestamp", "host", "method", "status", "decision", "matched_rule", "bytes_out", "bytes_in"):
        assert field in record
    assert record["sandbox"] == "ppp-red-bird"
    assert record["host"] == "api.anthropic.com"
    assert record["method"] == "POST"
    # No secret material or raw header values in the log line.
    assert "FAKE-KEY-123" not in log_text
    assert "secret-token" not in log_text
    assert "client-supplied" not in log_text
    assert "Bearer" not in log_text


def test_build_flow_log_shape():
    rec = addon.build_flow_log("s1", "example.com", "GET", 200, "allow", "rule-1", 10, 20)
    assert rec["sandbox"] == "s1"
    assert rec["decision"] == "allow"
    assert rec["matched_rule"] == "rule-1"
    assert rec["bytes_out"] == 10 and rec["bytes_in"] == 20
