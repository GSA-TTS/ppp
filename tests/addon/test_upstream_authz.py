"""Unit tests for the upstream interception-CA authorization helpers (ADR-0006).

These test the pure crypto logic that decides whether a presented TLS chain is
authorized by the host OS trust store — no live handshake. They build a small CA
hierarchy with the `cryptography` library and exercise _verify_signed_by,
_parse_pem_certs, and _chain_authorized_by_os_store via a stub addon.
"""

import datetime

import pytest

addon = pytest.importorskip("addon")
x509 = pytest.importorskip("cryptography.x509")
from cryptography.hazmat.primitives import hashes, serialization  # noqa: E402
from cryptography.hazmat.primitives.asymmetric import rsa  # noqa: E402
from cryptography.x509.oid import NameOID  # noqa: E402


def _key():
    return rsa.generate_private_key(public_exponent=65537, key_size=2048)


def _name(cn):
    return x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, cn)])


def _make_cert(cn, issuer_cn, issuer_key, subject_key, is_ca=True, self_signed=False):
    now = datetime.datetime.now(datetime.timezone.utc)
    builder = (
        x509.CertificateBuilder()
        .subject_name(_name(cn))
        .issuer_name(_name(issuer_cn))
        .public_key(subject_key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(days=1))
        .not_valid_after(now + datetime.timedelta(days=365))
        .add_extension(x509.BasicConstraints(ca=is_ca, path_length=None), critical=True)
    )
    signing_key = subject_key if self_signed else issuer_key
    return builder.sign(signing_key, hashes.SHA256())


def _pem(cert):
    return cert.public_bytes(serialization.Encoding.PEM)


def _der(cert):
    return cert.public_bytes(serialization.Encoding.DER)


def test_verify_signed_by_true_for_real_issuer():
    root_key = _key()
    root = _make_cert("Root", "Root", root_key, root_key, self_signed=True)
    inter_key = _key()
    inter = _make_cert("Intermediate", "Root", root_key, inter_key)
    assert addon._verify_signed_by(inter, root) is True


def test_verify_signed_by_false_for_wrong_issuer():
    root_key = _key()
    other_key = _key()
    other = _make_cert("Other", "Other", other_key, other_key, self_signed=True)
    inter_key = _key()
    inter = _make_cert("Intermediate", "Root", root_key, inter_key)
    # `other` did not sign `inter`.
    assert addon._verify_signed_by(inter, other) is False


def test_parse_pem_certs_roundtrip_and_skips_garbage():
    k = _key()
    c1 = _make_cert("A", "A", k, k, self_signed=True)
    c2 = _make_cert("B", "B", k, k, self_signed=True)
    blob = _pem(c1) + b"\nnot a cert\n" + _pem(c2)
    parsed = addon._parse_pem_certs(blob)
    cns = sorted(a.value for c in parsed for a in c.subject if a.oid == NameOID.COMMON_NAME)
    assert cns == ["A", "B"]


def _addon_with_store(store_certs):
    a = addon.PppAddon(data_dir="/tmp", config_dir="/tmp")
    a._os_store_cache = store_certs  # inject the "OS trust store"
    return a


def test_chain_authorized_when_intermediate_signed_by_store_root():
    root_key = _key()
    root = _make_cert("Zscaler Root", "Zscaler Root", root_key, root_key, self_signed=True)
    inter_key = _key()
    inter = _make_cert("Zscaler Intermediate", "Zscaler Root", root_key, inter_key)
    a = _addon_with_store([root])  # host trusts the root
    # presented chain (DER): [leaf-ish, intermediate] — intermediate is issued by store root
    assert a._chain_authorized_by_os_store([_der(inter)]) is True


def test_chain_not_authorized_when_nothing_ties_to_store():
    root_key = _key()
    store_root = _make_cert("Corp Root", "Corp Root", root_key, root_key, self_signed=True)
    rogue_key = _key()
    rogue = _make_cert("Rogue CA", "Rogue CA", rogue_key, rogue_key, self_signed=True)
    a = _addon_with_store([store_root])
    # rogue chain does not tie back to the store -> not authorized (fails closed)
    assert a._chain_authorized_by_os_store([_der(rogue)]) is False


def test_chain_not_authorized_with_empty_store(monkeypatch):
    root_key = _key()
    inter_key = _key()
    inter = _make_cert("Intermediate", "Root", root_key, inter_key)
    a = addon.PppAddon(data_dir="/tmp", config_dir="/tmp")
    # Force the OS-store load to yield nothing (transient/unreadable). Per the S3
    # fix an empty result is not cached, so patch the loader itself; the
    # authorization must fail closed.
    monkeypatch.setattr(addon, "_load_os_trust_store", lambda: [])
    assert a._chain_authorized_by_os_store([_der(inter)]) is False
