"""T11 guard tests: DNS-rebind defense and exfil-size gating."""

from __future__ import annotations

import json

import addon
from fakes import FakeFlow, FakeRequest, FakeResponse


def _addon_allow_all(tmp_path, resolver):
    (tmp_path / "port-registry.json").write_text(json.dumps({"ports": {"51820": {"sandbox": "ppp-red-bird", "state": "active"}}}))
    sb = tmp_path / "sandboxes" / "ppp-red-bird"
    sb.mkdir(parents=True)
    (sb / "policy.yaml").write_text("default: allow\nrules: []\n")
    a = addon.PppAddon(data_dir=str(tmp_path), config_dir=str(tmp_path), resolver=resolver)
    a.reload()
    return a


def test_is_forbidden_ip():
    assert addon.is_forbidden_ip("169.254.169.254")
    assert addon.is_forbidden_ip("10.0.0.5")
    assert addon.is_forbidden_ip("127.0.0.1")
    assert addon.is_forbidden_ip("192.168.1.1")
    assert addon.is_forbidden_ip("::1")
    assert addon.is_forbidden_ip("not-an-ip")  # fail closed
    assert not addon.is_forbidden_ip("93.184.216.34")  # public


def test_rebind_to_metadata_blocked(tmp_path):
    a = _addon_allow_all(tmp_path, resolver=lambda host: "169.254.169.254")
    flow = FakeFlow(51820, FakeRequest("evil.example.com"))
    a.request(flow)
    assert flow.response is not None
    assert flow.response.status_code == 403


def test_rebind_to_private_blocked(tmp_path):
    a = _addon_allow_all(tmp_path, resolver=lambda host: "10.1.2.3")
    flow = FakeFlow(51820, FakeRequest("evil.example.com"))
    a.request(flow)
    assert flow.response.status_code == 403


def test_rebind_to_loopback_blocked(tmp_path):
    a = _addon_allow_all(tmp_path, resolver=lambda host: "127.0.0.1")
    flow = FakeFlow(51820, FakeRequest("evil.example.com"))
    a.request(flow)
    assert flow.response.status_code == 403


def test_public_ip_not_blocked(tmp_path):
    a = _addon_allow_all(tmp_path, resolver=lambda host: "93.184.216.34")
    flow = FakeFlow(51820, FakeRequest("unknown.example.com"))
    a.request(flow)
    assert flow.response is None


def test_resolution_failure_fails_closed(tmp_path):
    # A non-IP host whose resolution fails must be blocked, not injected
    # (code review S5): a malicious guest could otherwise force local resolver
    # failure to bypass the rebind guard.
    a = _addon_allow_all(tmp_path, resolver=lambda host: None)
    flow = FakeFlow(51820, FakeRequest("unknown.example.com"))
    a.request(flow)
    assert flow.response is not None
    assert flow.response.status_code == addon.REBIND_STATUS


def test_any_forbidden_record_blocks(tmp_path):
    # If resolution returns multiple records and ANY is forbidden, block
    # (code review S5): a mixed public+private answer must not slip through.
    a = _addon_allow_all(tmp_path, resolver=lambda host: ["93.184.216.34", "169.254.169.254"])
    flow = FakeFlow(51820, FakeRequest("unknown.example.com"))
    a.request(flow)
    assert flow.response is not None
    assert flow.response.status_code == addon.REBIND_STATUS


def test_all_public_records_allowed(tmp_path):
    a = _addon_allow_all(tmp_path, resolver=lambda host: ["93.184.216.34", "93.184.216.35"])
    flow = FakeFlow(51820, FakeRequest("unknown.example.com"))
    a.request(flow)
    assert flow.response is None


def test_request_over_1mb_gets_413(tmp_path):
    a = _addon_allow_all(tmp_path, resolver=lambda host: "93.184.216.34")
    big = b"x" * (1 * 1024 * 1024 + 1)
    flow = FakeFlow(51820, FakeRequest("unknown.example.com", method="POST", content=big))
    a.request(flow)
    assert flow.response is not None
    assert flow.response.status_code == 413


def test_response_over_10mb_gets_413(tmp_path):
    a = _addon_allow_all(tmp_path, resolver=lambda host: "93.184.216.34")
    big = b"y" * (10 * 1024 * 1024 + 1)
    flow = FakeFlow(51820, FakeRequest("unknown.example.com"), response=FakeResponse(200, big))
    a.response(flow)
    assert flow.response.status_code == 413
