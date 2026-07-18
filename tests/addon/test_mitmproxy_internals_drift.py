"""B5 drift guard: fail loudly if the mitmproxy internals ppp replicates change.

The addon's tls_start_server hook (assets/addon.py) reconstructs mitmproxy's
upstream-TLS setup using internal APIs (net.tls.create_proxy_server_context and
the SNI/hostname logic in addons.tlsconfig.TlsConfig.tls_start_server, plus the
DEFAULT_HOSTFLAGS constant). If any of that upstream code changes, our replica
can silently drift. This test hashes the source of exactly those pieces and
compares to golden values; a mismatch means: go diff the upstream change,
re-sync assets/addon.py, and update the golden hash below.

Deliberately NOT pinned to mitmproxy.__version__: a version bump that does not
touch this code should not fail (per project decision). The source hash is the
real signal.

Note: a passing hash proves the upstream source is byte-identical, NOT that our
addon is behaviorally equivalent — the addon only *partially* mirrors
TlsConfig.tls_start_server (it intentionally omits the h2-ALPN-stripping and
ciphers_server handling). The test is a re-review *trigger*: when it fails, diff
upstream and decide whether the addon replica needs updating.
"""

import hashlib
import inspect

import pytest

net_tls = pytest.importorskip("mitmproxy.net.tls")
tlsconfig = pytest.importorskip("mitmproxy.addons.tlsconfig")


def _hash(obj) -> str:
    return hashlib.sha256(inspect.getsource(obj).encode()).hexdigest()


# Golden hashes captured against mitmproxy 12.2.3 (the pinned version). When one
# of these fails after a mitmproxy upgrade: read the upstream diff, re-sync
# assets/addon.py._build_upstream_ssl_conn to match, then update the hash here.
GOLDEN = {
    "create_proxy_server_context":
        "e2d90d71c574479781516eb28370a3de16f7995456f3a640b6f7cc2b07454929",
    "_create_ssl_context":
        "d322b556bd36254fca8cd0f88fdcccc762aa58bdd7526fbf7003eec62be36c4b",
    "TlsConfig.tls_start_server":
        "2e2dc9c25670cf646b0473ac4dd043dfb06b1ee2a1f58879e484d112acc0a55a",
}


def test_create_proxy_server_context_unchanged():
    assert _hash(net_tls.create_proxy_server_context) == GOLDEN["create_proxy_server_context"], (
        "mitmproxy.net.tls.create_proxy_server_context changed upstream; "
        "re-sync assets/addon.py._build_upstream_ssl_conn and update the golden hash."
    )


def test_create_ssl_context_unchanged():
    assert _hash(net_tls._create_ssl_context) == GOLDEN["_create_ssl_context"], (
        "mitmproxy.net.tls._create_ssl_context changed upstream; "
        "re-review the upstream TLS context construction and update the golden hash."
    )


def test_tlsconfig_tls_start_server_unchanged():
    assert _hash(tlsconfig.TlsConfig.tls_start_server) == GOLDEN["TlsConfig.tls_start_server"], (
        "mitmproxy TlsConfig.tls_start_server changed upstream; the addon mirrors its "
        "SNI/hostname + ssl_conn pre-emption logic — re-sync and update the golden hash."
    )


def test_default_hostflags_match_replica():
    # The addon hardcodes _UPSTREAM_HOSTFLAGS = 0x4 | 0x20; assert it still equals
    # mitmproxy's DEFAULT_HOSTFLAGS so our upstream hostname policy is not looser.
    assert tlsconfig.DEFAULT_HOSTFLAGS == (0x4 | 0x20)
