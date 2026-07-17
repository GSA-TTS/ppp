"""T11 secret injection + cache tests against a fake UDS parent."""

from __future__ import annotations

import json

import addon
from fakes import FakeFlow, FakeRequest, FakeUdsServer


def _setup(tmp_path, handler, policy="default: allow\nrules: []\n"):
    (tmp_path / "port-registry.json").write_text(json.dumps({"51820": "ppp-red-bird"}))
    sb = tmp_path / "sandboxes" / "ppp-red-bird"
    sb.mkdir(parents=True)
    (sb / "policy.yaml").write_text(policy)
    server = FakeUdsServer(str(tmp_path / "secret.sock"), handler)
    a = addon.PppAddon(
        data_dir=str(tmp_path),
        config_dir=str(tmp_path),
        resolver=lambda host: "93.184.216.34",  # public IP; bypass rebind guard
    )
    a.reload()
    return a, server


def test_service_hit_injects_and_strips(tmp_path):
    def handler(req):
        assert req == {"service": "anthropic", "sandbox": "ppp-red-bird", "host": "api.anthropic.com"} or req["service"] == ""
        if req.get("service") == "anthropic":
            return {"ok": True, "header": "x-api-key", "value": "FAKE-KEY-123"}
        return {"ok": True, "substitutions": []}

    a, server = _setup(tmp_path, handler)
    try:
        flow = FakeFlow(
            51820,
            FakeRequest(
                "api.anthropic.com",
                headers={"x-api-key": "client-supplied", "Authorization": "Bearer evil"},
            ),
        )
        a.request(flow)
        # Injected value replaces the client's; auth header stripped.
        assert flow.request.headers["x-api-key"] == "FAKE-KEY-123"
        assert "authorization" not in flow.request.headers
        assert flow.response is None  # allowed, not blocked
    finally:
        server.close()


def test_miss_injects_nothing(tmp_path):
    def handler(req):
        if req.get("service"):
            return {"ok": False, "reason": "no-secret"}
        return {"ok": True, "substitutions": []}

    a, server = _setup(tmp_path, handler)
    try:
        flow = FakeFlow(51820, FakeRequest("api.anthropic.com"))
        a.request(flow)
        assert "x-api-key" not in flow.request.headers
        assert flow.response is None
    finally:
        server.close()


def test_locked_injects_nothing(tmp_path):
    def handler(req):
        if req.get("service"):
            return {"ok": False, "reason": "locked"}
        return {"ok": True, "substitutions": []}

    a, server = _setup(tmp_path, handler)
    try:
        flow = FakeFlow(51820, FakeRequest("api.anthropic.com", headers={"x-api-key": "keep"}))
        a.request(flow)
        # No injection; but the strip only happens on a hit, so the client
        # header remains (we did not overwrite with a wrong value).
        assert flow.request.headers.get("x-api-key") == "keep"
        assert flow.response is None
    finally:
        server.close()


def test_custom_substitution_applied(tmp_path):
    def handler(req):
        if req.get("service"):
            return {"ok": False, "reason": "no-secret"}
        return {"ok": True, "substitutions": [{"placeholder": "__TOKEN__", "value": "FAKE-SUB-9"}]}

    a, server = _setup(tmp_path, handler)
    try:
        flow = FakeFlow(
            51820,
            FakeRequest("api.anthropic.com", headers={"X-Custom": "prefix __TOKEN__ suffix"}),
        )
        a.request(flow)
        assert flow.request.headers["X-Custom"] == "prefix FAKE-SUB-9 suffix"
    finally:
        server.close()


def test_unmapped_host_runs_custom_only(tmp_path):
    seen = []

    def handler(req):
        seen.append(req)
        return {"ok": True, "substitutions": []}

    a, server = _setup(tmp_path, handler)
    try:
        flow = FakeFlow(51820, FakeRequest("unknown.example.com"))
        a.request(flow)
        # Only the custom path (empty service) is queried for an unmapped host.
        assert all(r["service"] == "" for r in seen)
        assert seen and seen[0]["host"] == "unknown.example.com"
    finally:
        server.close()


def test_cache_hits_uds_once_then_sighup_requery(tmp_path):
    def handler(req):
        if req.get("service") == "anthropic":
            return {"ok": True, "header": "x-api-key", "value": "FAKE-KEY-123"}
        return {"ok": True, "substitutions": []}

    a, server = _setup(tmp_path, handler)
    try:
        for _ in range(2):
            flow = FakeFlow(51820, FakeRequest("api.anthropic.com"))
            a.request(flow)
        # Two requests, same (service,sandbox,host): the anthropic lookup is
        # cached, so it is queried once. (The empty-service custom lookup is a
        # different cache key, also queried once.) Count anthropic queries:
        anthropic_queries = [r for r in server.requests if r.get("service") == "anthropic"]
        assert len(anthropic_queries) == 1

        # SIGHUP-equivalent cache clear forces a re-query.
        a.reload()
        flow = FakeFlow(51820, FakeRequest("api.anthropic.com"))
        a.request(flow)
        anthropic_queries = [r for r in server.requests if r.get("service") == "anthropic"]
        assert len(anthropic_queries) == 2
    finally:
        server.close()


def test_uds_error_no_injection(tmp_path):
    # Point at a non-existent socket: uds_query returns None → no injection,
    # request still proceeds (allowed by policy).
    (tmp_path / "port-registry.json").write_text(json.dumps({"51820": "ppp-red-bird"}))
    sb = tmp_path / "sandboxes" / "ppp-red-bird"
    sb.mkdir(parents=True)
    (sb / "policy.yaml").write_text("default: allow\nrules: []\n")
    a = addon.PppAddon(
        data_dir=str(tmp_path),
        config_dir=str(tmp_path),
        resolver=lambda host: "93.184.216.34",
    )
    a.reload()
    flow = FakeFlow(51820, FakeRequest("api.anthropic.com", headers={"x-api-key": "keep"}))
    a.request(flow)
    assert flow.request.headers.get("x-api-key") == "keep"
    assert flow.response is None
