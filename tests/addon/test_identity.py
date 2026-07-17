"""T11 identity tests: sandbox identity comes ONLY from the WireGuard listen port."""

from __future__ import annotations

import os

import addon
from fakes import FakeClient, FakeFlow, FakeRequest


def _make_addon(tmp_path, port_map):
    reg = tmp_path / "port-registry.json"
    import json

    reg.write_text(json.dumps(port_map))
    a = addon.PppAddon(data_dir=str(tmp_path), config_dir=str(tmp_path))
    a.reload()
    return a


def test_listen_port_resolves_sandbox(tmp_path):
    a = _make_addon(tmp_path, {"51820": "ppp-red-bird"})
    flow = FakeFlow(51820, FakeRequest("api.anthropic.com"))
    assert a.sandbox_for(flow) == "ppp-red-bird"


def test_unknown_port_fails_closed(tmp_path):
    a = _make_addon(tmp_path, {"51820": "ppp-red-bird"})
    flow = FakeFlow(59999, FakeRequest("api.anthropic.com"))
    assert a.sandbox_for(flow) is None


def test_identity_ignores_inner_ip_and_sockname(tmp_path):
    # Two sandboxes; the flow arrives on red-bird's port but its inner
    # IP/sockname are set to blue-fox's coordinates. Identity MUST follow the
    # port (red-bird), proving the spoofable inner values are never consulted.
    a = _make_addon(tmp_path, {"51820": "ppp-red-bird", "51821": "ppp-blue-fox"})
    client = FakeClient(address=("10.0.0.2", 12345), sockname=("10.0.0.2", 443))
    flow = FakeFlow(51820, FakeRequest("api.anthropic.com"), client=client)
    assert a.sandbox_for(flow) == "ppp-red-bird"


def test_string_and_int_port_keys_tolerated(tmp_path):
    import json

    reg = tmp_path / "port-registry.json"
    reg.write_text(json.dumps({"51820": "ppp-red-bird"}))
    a = addon.PppAddon(data_dir=str(tmp_path), config_dir=str(tmp_path))
    a.reload()
    assert a._port_map[51820] == "ppp-red-bird"


def test_missing_registry_denies_all_ports(tmp_path):
    a = addon.PppAddon(data_dir=str(tmp_path), config_dir=str(tmp_path))
    a.reload()
    assert a.sandbox_for(FakeFlow(51820, FakeRequest("x"))) is None


def test_unreadable_listen_port_fails_closed(tmp_path):
    a = _make_addon(tmp_path, {"51820": "ppp-red-bird"})

    class Broken:
        pass

    assert a.sandbox_for(Broken()) is None
    assert not os.path.exists(os.path.join(str(tmp_path), "does-not-matter"))
