"""Regression test for code-review B1: the upstream verify callback must be
installable on repeated flows.

create_proxy_server_context is @lru_cache'd, so every flow reuses the same
SSL.Context, and pyOpenSSL forbids mutating a Context after a Connection has been
created from it. The addon therefore sets the verify callback on the CONNECTION,
not the Context. This test reproduces the original failure mode (mutating the
cached Context after first use raises) and confirms the connection-level path
works across many flows.
"""

import pytest

net_tls = pytest.importorskip("mitmproxy.net.tls")
SSL = pytest.importorskip("OpenSSL.SSL")
addon = pytest.importorskip("addon")


def _cached_ctx():
    return net_tls.create_proxy_server_context(
        method=net_tls.Method.TLS_CLIENT_METHOD,
        min_version=net_tls.Version.UNBOUNDED,
        max_version=net_tls.Version.UNBOUNDED,
        cipher_list=None,
        ecdh_curve=None,
        verify=net_tls.Verify.VERIFY_PEER,
        ca_path=None,
        ca_pemfile=None,
        client_cert=None,
        legacy_server_connect=False,
    )


def test_factory_is_cached_same_context():
    # Establishes the premise: the factory returns the SAME Context object for
    # identical args, which is why Context-level mutation after first use fails.
    assert _cached_ctx() is _cached_ctx()


def test_context_level_set_verify_fails_after_first_connection():
    # Documents the original B1 defect: mutating the cached Context after a
    # Connection exists raises. (If a future pyOpenSSL relaxes this, this test
    # will start failing and we can simplify — but the connection-level fix below
    # is correct regardless.)
    ctx = _cached_ctx()
    a = addon.PppAddon(data_dir="/tmp", config_dir="/tmp")
    SSL.Connection(ctx)  # first use of the cached context
    with pytest.raises(ValueError):
        ctx.set_verify(SSL.VERIFY_PEER, a._make_upstream_verify_cb())


def test_connection_level_set_verify_works_repeatedly():
    # The fix: set_verify on each Connection succeeds across many flows sharing
    # the cached Context.
    a = addon.PppAddon(data_dir="/tmp", config_dir="/tmp")
    for _ in range(5):
        conn = SSL.Connection(_cached_ctx())
        conn.set_verify(SSL.VERIFY_PEER, a._make_upstream_verify_cb())  # must not raise
