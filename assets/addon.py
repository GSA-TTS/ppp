"""ppp mitmproxy addon: per-sandbox network policy + host-side secret injection.

This is the security-critical enforcement point of ppp (Podman Plus Proxy).
A single mitmdump process fronts every sandbox; each sandbox is a distinct
WireGuard instance (unique UDP listen port + keypair) inside that process. This
addon:

  1. Identifies which sandbox a flow belongs to by the receiving WireGuard
     instance's *listen port* (``flow.client_conn.proxy_mode.listen_port()`` on
     mitmproxy 12.2.3). The listen port is cryptographically bound to a
     per-sandbox keypair and cannot be forged from inside the guest. The inner
     tunnel IP is spoofable by a sudo-capable agent and is therefore NEVER used
     for identity (spec docs/explorations/ppp-spec.md §3.1, ADR-0003).
  2. Evaluates that sandbox's network policy with deny-wins precedence, mirroring
     the Go ``internal/policy`` engine (T4). A deny (or an unknown sandbox, or a
     malformed policy) fails closed with an HTTP 403.
  3. On an allow, queries the Go parent over a Unix domain socket for the secret
     to inject, mirroring the T8 UDS wire protocol (newline-delimited JSON, one
     request/response per connection), strips any client-supplied credential
     header, and injects the host-held value so the agent never sees the key.
  4. Applies DNS-rebinding and exfil-size guards, and writes a redacted
     per-flow log line to ``$PPP_DATA/flows.jsonl``.

The addon is deliberately split into small pure helpers (policy eval, host→
service map, redaction, guard checks, UDS query) so the enforcement logic is
unit-testable without a live mitmproxy runtime. The mitmproxy hook methods
(``request``/``response``/lifecycle) are thin wrappers over those helpers.
"""

from __future__ import annotations

import fnmatch
import ipaddress
import json
import logging
import os
import re
import signal
import socket
import time
from typing import Callable, Dict, List, Optional, Tuple

try:  # pragma: no cover - exercised only under a real mitmproxy runtime
    from mitmproxy import http
except Exception:  # pragma: no cover - allow import under pure unit tests
    http = None  # type: ignore[assignment]

_logger = logging.getLogger("ppp.addon")

try:
    from ruamel.yaml import YAML

    _yaml = YAML(typ="safe")
except Exception:  # pragma: no cover - yaml is always present with mitmproxy
    _yaml = None


# --- Constants ---------------------------------------------------------------

BLOCK_STATUS = 403
"""HTTP status returned when policy denies a flow (fail-closed default)."""

REBIND_STATUS = 403
"""HTTP status returned when a host resolves to a private/metadata IP."""

TOO_LARGE_STATUS = 413
"""HTTP status returned when a payload exceeds the exfil-size limits."""

MAX_REQUEST_BYTES = 1 * 1024 * 1024  # 1 MB outbound cap (exfil gating).
MAX_RESPONSE_BYTES = 10 * 1024 * 1024  # 10 MB inbound cap (exfil gating).

# OpenSSL X509_V_FLAG_PARTIAL_CHAIN: allow a trusted non-self-signed cert (the
# conformant interception intermediate) to serve as the chain anchor. Used only
# on the upstream (proxy->server) verification, never to disable verification.
_PARTIAL_CHAIN_FLAG = 0x80000

# X509_CHECK_FLAG_NO_PARTIAL_WILDCARDS — the hostname-match flag mitmproxy's
# TlsConfig sets on upstream verification; mirrored here so our context matches
# mitmproxy's hostname policy exactly.
_HOSTFLAG_NO_PARTIAL_WILDCARDS = 0x4

CACHE_TTL_SECONDS = 60.0
"""Lifetime of a cached UDS lookup, keyed by (service, sandbox, host)."""

UDS_TIMEOUT_SECONDS = 5.0
"""Socket timeout for a single UDS request/response round trip."""

UDS_MAX_REPLY_BYTES = 64 * 1024
"""Cap on a single UDS reply line, matching the T8 server's request bound."""

_BLOCK_ALL = "**"

# Regex metacharacters whose presence marks a resource as a regex rather than a
# glob. Glob's own '*'/'?' are deliberately excluded so "*.example.com" is a
# glob, not a regex (mirrors internal/policy/rule.go regexMetachars EXACTLY).
_REGEX_METACHARS = set(r"^$()[]{}+|\\")

# Credential headers that must never survive from the client for an injected
# service. The agent must not be able to supply or override the host-held
# credential, so these are stripped before injection regardless of the scheme.
_STRIP_HEADERS = ("authorization", "x-api-key", "x-goog-api-key", "x-github-token")


# --- Host -> service map (reviewable data) -----------------------------------
#
# Maps a request host to a known service name used in the UDS lookup. Kept as a
# small, human-reviewable table. A host absent here has no known service, so the
# addon runs only the custom-substitution path for it (service left empty).
HOST_SERVICE_MAP: Dict[str, str] = {
    "api.anthropic.com": "anthropic",
    "api.openai.com": "openai",
    "github.com": "github",
    "api.github.com": "github",
    "generativelanguage.googleapis.com": "google",
    "api.gsa.usai.gov": "usai",
}


def host_to_service(host: str) -> Optional[str]:
    """Return the known service for a request host, or None if unmapped."""
    return HOST_SERVICE_MAP.get(host.lower())


# --- Policy model (mirrors Go internal/policy semantics) ---------------------


class Rule:
    """A single compiled network policy rule.

    Construction parses the resource into a host matcher and an optional port
    once, up front. A rule whose resource fails to compile is invalid and is
    reported to the caller so the whole policy fails closed.
    """

    def __init__(self, decision: str, resource: str, rule_id: str = "") -> None:
        self.id = rule_id
        self.decision = decision  # "allow" | "deny"
        self.resource = resource
        self.port, self._matcher = _compile_resource(resource)

    def matches(self, host: str, port: int) -> bool:
        """Report whether (host, port) matches this rule's resource."""
        if self.port and self.port != port:
            return False
        return self._matcher(host)


class Policy:
    """A compiled, evaluatable policy: ordered rules + a default decision.

    The zero/empty policy denies everything: an absent or non-"allow" default
    normalizes to deny, so evaluation always fails closed.
    """

    def __init__(self, rules: List[Rule], default: str) -> None:
        self.rules = rules
        self.default = "allow" if default == "allow" else "deny"

    def evaluate(self, host: str, port: int) -> Tuple[str, Optional[Rule]]:
        """Return (decision, matched_rule) with deny-wins precedence.

        Deny > allow > default: the first matching deny wins; else the first
        matching allow; else the policy default. matched_rule is None when the
        default applied.
        """
        for r in self.rules:
            if r.decision == "deny" and r.matches(host, port):
                return "deny", r
        for r in self.rules:
            if r.decision == "allow" and r.matches(host, port):
                return "allow", r
        return self.default, None


def _deny_all() -> Policy:
    """A policy that denies everything (returned on any load failure)."""
    return Policy([], "deny")


def _is_regex(host: str) -> bool:
    """Report whether host contains a non-glob regex metacharacter."""
    return any(ch in _REGEX_METACHARS for ch in host)


# A resource matcher takes a host and returns whether it matches. The compile
# step below picks block-all, IP/CIDR, regex, or glob — mirroring Go's
# compileHostMatcher.
Matcher = Callable[[str], bool]


def _compile_resource(resource: str) -> Tuple[int, Matcher]:
    """Compile a rule resource into (port, matcher).

    port is 0 when the resource does not constrain a port. The matcher applies
    the same strategy selection as Go's internal/policy: block-all ('**'),
    IP/CIDR literal, auto-detected regex, else glob (subdomain-only for
    '*.domain'). A malformed resource yields a matcher that never matches
    (fail closed).
    """
    res = resource.strip()
    if res == _BLOCK_ALL:
        return 0, lambda _host: True
    host, port = _split_resource_port(res)
    if host == "":
        return 0, lambda _host: False

    ip_matcher = _compile_ip_matcher(host)
    if ip_matcher is not None:
        return port, ip_matcher
    if _is_regex(host):
        return port, _compile_regex_matcher(host)
    return port, _compile_glob_matcher(host)


def _split_resource_port(res: str) -> Tuple[str, int]:
    """Split an optional ':port' suffix off a resource host.

    Block-all and CIDR forms are never port-split. Bracketed IPv6 with a port
    ('[::1]:443') is supported; a bare IPv6 literal keeps its colons. A
    malformed/out-of-range port yields host="" so the rule fails closed.
    """
    if res == _BLOCK_ALL or "/" in res:
        return res, 0
    if res.startswith("["):
        end = res.find("]")
        if end < 0:
            return res, 0
        host = res[1:end]
        rest = res[end + 1 :]
        if rest.startswith(":"):
            return _with_port(host, rest[1:])
        return host, 0
    if res.count(":") == 1:
        host, _, port_str = res.partition(":")
        return _with_port(host, port_str)
    return res, 0


def _with_port(host: str, port_str: str) -> Tuple[str, int]:
    """Validate a port string, failing closed (host="") when out of range."""
    try:
        port = int(port_str)
    except ValueError:
        return "", 0
    if port < 1 or port > 65535 or host == "":
        return "", 0
    return host, port


def _compile_ip_matcher(host: str) -> Optional[Matcher]:
    """Return a matcher for an IP literal or CIDR host, else None.

    Returns None when host is not an IP form (caller falls back to glob/regex).
    A malformed CIDR fails closed with a never-match matcher.
    """
    if "/" in host:
        try:
            net = ipaddress.ip_network(host, strict=False)
        except ValueError:
            return lambda _host: False
        return lambda h: _addr_in_net(h, net)
    try:
        addr = ipaddress.ip_address(host)
    except ValueError:
        return None
    return lambda h: _addr_equals(h, addr)


def _addr_in_net(host: str, net: "ipaddress._BaseNetwork") -> bool:
    try:
        return ipaddress.ip_address(host) in net
    except ValueError:
        return False


def _addr_equals(host: str, addr: "ipaddress._BaseAddress") -> bool:
    try:
        return ipaddress.ip_address(host) == addr
    except ValueError:
        return False


def _compile_regex_matcher(host: str) -> Matcher:
    """Compile an anchored, case-insensitive regex matcher (fail closed)."""
    pattern = host
    if not pattern.startswith("^"):
        pattern = "^" + pattern
    if not pattern.endswith("$"):
        pattern = pattern + "$"
    try:
        rx = re.compile(pattern, re.IGNORECASE)
    except re.error:
        return lambda _host: False
    return lambda h: rx.match(h) is not None


def _compile_glob_matcher(host: str) -> Matcher:
    """Compile a case-insensitive glob matcher.

    Per spec §5.5 a '*.example.com' wildcard matches SUBDOMAINS only, never the
    apex 'example.com'. fnmatch's '*' would match the apex (empty label), so
    the apex is explicitly excluded for a leading-'*.' pattern.
    """
    pat = host.lower()
    subdomain_only = pat.startswith("*.")
    apex = pat[2:] if subdomain_only else ""

    def match(h: str) -> bool:
        hl = h.lower()
        if subdomain_only and hl == apex:
            return False
        return fnmatch.fnmatchcase(hl, pat)

    return match


def parse_policy(data: object) -> Policy:
    """Compile a parsed YAML mapping into a Policy, failing closed on error.

    An empty/None document, a non-mapping, a missing default, or any
    unparseable rule yields a deny-all policy. A missing default is deny, never
    allow (mirrors Go parseDefault).
    """
    if not isinstance(data, dict):
        return _deny_all()
    default_raw = data.get("default", "")
    default = "allow" if default_raw in ("allow", "allow-all") else "deny"
    rules_raw = data.get("rules") or []
    if not isinstance(rules_raw, list):
        return _deny_all()
    rules: List[Rule] = []
    for rd in rules_raw:
        if not isinstance(rd, dict):
            return _deny_all()
        decision = rd.get("decision", "")
        resource = rd.get("resource", "")
        if decision not in ("allow", "deny") or not isinstance(resource, str) or resource.strip() == "":
            return _deny_all()
        rules.append(Rule(decision, resource, str(rd.get("id", ""))))
    return Policy(rules, default)


def _load_yaml_file(path: str) -> object:
    """Load a YAML file into a Python object, returning None on any failure."""
    if _yaml is None:
        return None
    try:
        with open(path, "r", encoding="utf-8") as fh:
            return _yaml.load(fh)
    except Exception:  # noqa: BLE001 - fail closed on any read/parse error (incl. YAML)
        return None


# Sentinel: a policy file exists on disk but could not be read/parsed. Distinct
# from None (file absent), because a *corrupt* per-sandbox policy MUST fail
# closed (deny that sandbox) rather than silently fall back to a possibly
# permissive global policy (see code review S4).
_UNPARSEABLE = object()


def _load_policy_doc(path: str) -> object:
    """Load a policy YAML doc, distinguishing absent from present-but-corrupt.

    Returns None if the file does not exist, _UNPARSEABLE if it exists but
    cannot be read/parsed (or PyYAML is unavailable), else the parsed object.
    """
    if not os.path.exists(path):
        return None
    if _yaml is None:
        return _UNPARSEABLE
    try:
        with open(path, "r", encoding="utf-8") as fh:
            return _yaml.load(fh)
    except Exception:  # noqa: BLE001 - present but unreadable/unparseable → fail closed
        return _UNPARSEABLE


def load_policy_for(
    sandbox: str,
    data_dir: str,
    global_policy_path: str,
) -> Policy:
    """Load and merge the per-sandbox and global policy for a sandbox.

    Per-sandbox rules ($PPP_DATA/sandboxes/<name>/policy.yaml) are evaluated
    before the global rules ($PPP_CONFIG/policies.yaml); the sandbox default
    governs when set, else the global default. Any load/parse failure fails
    closed (deny-all). A per-sandbox policy file that exists but cannot be
    parsed fails closed for THAT sandbox (deny-all) rather than falling back to
    the global policy, which could be more permissive (code review S4).
    """
    sandbox_path = os.path.join(data_dir, "sandboxes", sandbox, "policy.yaml")
    sandbox_doc = _load_policy_doc(sandbox_path)
    global_doc = _load_policy_doc(global_policy_path)

    # A present-but-corrupt sandbox policy denies everything for this sandbox —
    # never silently inherit the (possibly permissive) global policy.
    if sandbox_doc is _UNPARSEABLE:
        return _deny_all()
    # A corrupt global policy also fails closed.
    if global_doc is _UNPARSEABLE:
        return _deny_all()

    if sandbox_doc is None and global_doc is None:
        return _deny_all()

    sandbox_pol = parse_policy(sandbox_doc) if sandbox_doc is not None else None
    global_pol = parse_policy(global_doc) if global_doc is not None else None

    rules: List[Rule] = []
    if sandbox_pol is not None:
        rules.extend(sandbox_pol.rules)
    if global_pol is not None:
        rules.extend(global_pol.rules)

    # Default precedence: an explicit sandbox default wins; else the global
    # default; else deny. parse_policy already normalizes to allow/deny.
    if sandbox_doc is not None and isinstance(sandbox_doc, dict) and "default" in sandbox_doc:
        default = sandbox_pol.default  # type: ignore[union-attr]
    elif global_pol is not None:
        default = global_pol.default
    else:
        default = "deny"
    return Policy(rules, default)


# --- Secret UDS client (mirrors T8 wire protocol) ----------------------------


def uds_query(socket_path: str, request: Dict[str, str]) -> Optional[dict]:
    """Send one newline-delimited JSON request to the UDS and read one reply.

    Connect, send exactly one line, read exactly one line, close (matching the
    T8 server's one-request-per-connection contract). Returns the decoded
    response dict, or None on any transport/parse error (fail closed → no
    injection). Never raises.
    """
    line = (json.dumps(request, separators=(",", ":")) + "\n").encode("utf-8")
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as sock:
            sock.settimeout(UDS_TIMEOUT_SECONDS)
            sock.connect(socket_path)
            sock.sendall(line)
            buf = bytearray()
            while b"\n" not in buf:
                chunk = sock.recv(4096)
                if not chunk:
                    break
                buf.extend(chunk)
                if len(buf) > UDS_MAX_REPLY_BYTES:
                    return None
    except OSError:
        return None
    raw, _, _ = bytes(buf).partition(b"\n")
    if not raw:
        return None
    try:
        resp = json.loads(raw.decode("utf-8"))
    except (ValueError, UnicodeDecodeError):
        return None
    return resp if isinstance(resp, dict) else None


# --- DNS-rebind guard --------------------------------------------------------


def is_forbidden_ip(ip_str: str) -> bool:
    """Report whether an IP is private/loopback/link-local/metadata.

    169.254.169.254 (cloud metadata) is covered by the link-local check but is
    called out explicitly for clarity. An unparseable value is treated as
    forbidden (fail closed).
    """
    try:
        addr = ipaddress.ip_address(ip_str)
    except ValueError:
        return True
    return (
        addr.is_private
        or addr.is_loopback
        or addr.is_link_local
        or addr.is_reserved
        or addr.is_multicast
        or addr == ipaddress.ip_address("169.254.169.254")
    )


def _default_resolver(host: str) -> Optional[List[str]]:
    """Resolve a host to all its IPs via getaddrinfo; None on failure.

    Returns every resolved address (not just the first) so the rebind guard can
    reject a response that mixes a public and a private/metadata record (code
    review S5). None signals resolution failure, which the guard treats as
    fail-closed for non-IP-literal hosts.
    """
    try:
        infos = socket.getaddrinfo(host, None)
    except OSError:
        return None
    addrs = [info[4][0] for info in infos if info[4] and info[4][0]]
    return addrs or None


# --- Redaction ---------------------------------------------------------------


def build_flow_log(
    sandbox: str,
    host: str,
    method: str,
    status: Optional[int],
    decision: str,
    matched_rule: str,
    bytes_out: int,
    bytes_in: int,
) -> Dict[str, object]:
    """Build the per-flow log record (spec §5.4).

    Only the fixed, non-sensitive fields are included. Secret values and
    credential header values are never passed in, so they cannot leak into the
    log line — redaction is by construction (the log carries no header values
    at all).
    """
    return {
        "sandbox": sandbox,
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "host": host,
        "method": method,
        "status": status,
        "decision": decision,
        "matched_rule": matched_rule,
        "bytes_out": bytes_out,
        "bytes_in": bytes_in,
    }


# --- Addon -------------------------------------------------------------------


class PppAddon:
    """The mitmproxy addon enforcing policy and injecting secrets per sandbox.

    Paths and the DNS resolver are injectable so the whole class is testable
    without a live mitmproxy runtime, real secrets, or real DNS.
    """

    def __init__(
        self,
        data_dir: Optional[str] = None,
        config_dir: Optional[str] = None,
        resolver: Optional[Callable[[str], "Optional[List[str] | str]"]] = None,
    ) -> None:
        self.data_dir = data_dir or os.environ.get("PPP_DATA", "")
        self.config_dir = config_dir or os.environ.get("PPP_CONFIG", "")
        self.socket_path = os.path.join(self.data_dir, "secret.sock")
        self.global_policy_path = os.path.join(self.config_dir, "policies.yaml")
        self.flow_log_path = os.path.join(self.data_dir, "flows.jsonl")
        self.port_registry_path = os.path.join(self.data_dir, "port-registry.json")
        self._resolve = resolver or _default_resolver

        self._port_map: Dict[int, str] = {}
        # UDS cache keyed by (service, sandbox, host) -> (response, expires_at).
        self._cache: Dict[Tuple[str, str, str], Tuple[dict, float]] = {}
        # ids of flows already logged in the request hook (denied/blocked), so
        # the response hook does not log them a second time when mitmproxy
        # fires it for the synthetic response.
        self._logged_ids: set = set()

    # -- lifecycle --

    def load(self, loader: object) -> None:  # pragma: no cover - mitmproxy hook
        """mitmproxy lifecycle hook: initial load of registry + SIGHUP wiring."""
        self.reload()
        try:
            signal.signal(signal.SIGHUP, self._on_sighup)
        except (ValueError, OSError):
            pass  # not on the main thread / unsupported platform; skip

    def _on_sighup(self, _signum: int, _frame: object) -> None:  # pragma: no cover
        self.reload()

    def tls_start_server(self, tls_start: object) -> None:  # pragma: no cover - real TLS
        """Build the UPSTREAM (proxy->server) TLS connection with partial-chain
        verification enabled, so ppp works on a TLS-inspecting network WITHOUT
        disabling verification.

        Why this is necessary (ADR-0006): mitmproxy verifies upstream certs with
        OpenSSL 3, which by default requires the chain to terminate at a
        self-signed root in the trust store. On a TLS-inspecting network (e.g.
        Zscaler) the presented chain anchors at a long-lived, RFC-conformant
        interception INTERMEDIATE, but the self-signed ROOT above it is
        non-conformant (BasicConstraints not marked critical) and OpenSSL 3
        rejects it. ppp's upstream CA bundle (internal/catrust) contains that
        conformant intermediate but drops the broken root, so OpenSSL fails with
        "unable to get issuer certificate" unless it is allowed to treat the
        trusted intermediate as the anchor. X509_V_FLAG_PARTIAL_CHAIN does
        exactly that.

        mitmproxy's built-in TlsConfig.tls_start_server has no option to set that
        flag, but it skips building when an addon already set tls_start.ssl_conn.
        So we build the connection here — using mitmproxy's own
        create_proxy_server_context so cipher/version/verify policy is identical
        — then add the PARTIAL_CHAIN flag and the standard SNI + hostname
        verification. Verification stays fully ON (never ssl_insecure); we only
        permit a trusted intermediate to anchor the chain. On a normal network
        the chain already ends at a conformant root, so partial-chain changes
        nothing and this remains correct.
        """
        if getattr(tls_start, "ssl_conn", None) is not None:
            return  # another addon already built it
        try:
            self._build_upstream_ssl_conn(tls_start)
        except Exception as exc:  # noqa: BLE001 - fall back to mitmproxy's builder
            self._log_warn("ppp: upstream TLS build failed, using default: %r" % (exc,))

    def _build_upstream_ssl_conn(self, tls_start: object) -> None:  # pragma: no cover - real TLS
        import ipaddress as _ip

        from mitmproxy import ctx as _ctx
        from mitmproxy.net import tls as _net_tls
        from OpenSSL import SSL as _SSL

        opts = _ctx.options
        if opts.ssl_insecure:
            return  # user explicitly disabled verification; don't second-guess
        ca_pemfile = opts.ssl_verify_upstream_trusted_ca
        if not ca_pemfile:
            return  # no ppp bundle configured; let mitmproxy's default build run

        server = tls_start.conn
        client = tls_start.context.client
        if server.sni is None:
            server.sni = client.sni or server.address[0]

        sslctx = _net_tls.create_proxy_server_context(
            method=(_net_tls.Method.DTLS_CLIENT_METHOD
                    if getattr(tls_start, "is_dtls", False)
                    else _net_tls.Method.TLS_CLIENT_METHOD),
            min_version=_net_tls.Version[opts.tls_version_server_min],
            max_version=_net_tls.Version[opts.tls_version_server_max],
            cipher_list=tuple(server.cipher_list) if server.cipher_list else None,
            ecdh_curve=_net_tls.get_curve(opts.tls_ecdh_curve_server),
            verify=_net_tls.Verify.VERIFY_PEER,
            ca_path=opts.ssl_verify_upstream_trusted_confdir,
            ca_pemfile=ca_pemfile,
            client_cert=None,
            legacy_server_connect=False,
        )
        # The one change vs. mitmproxy's default: allow a trusted non-root cert
        # (the conformant interception intermediate) to anchor the chain.
        sslctx.get_cert_store().set_flags(_PARTIAL_CHAIN_FLAG)

        conn = _SSL.Connection(sslctx)
        # SNI + hostname verification, mirroring TlsConfig.tls_start_server.
        param = _SSL._lib.SSL_get0_param(conn._ssl)
        _SSL._lib.X509_VERIFY_PARAM_set_hostflags(param, _HOSTFLAG_NO_PARTIAL_WILDCARDS)
        try:
            ip = _ip.ip_address(server.sni).packed
        except ValueError:
            host = server.sni.encode("idna")
            conn.set_tlsext_host_name(host)
            _SSL._openssl_assert(
                _SSL._lib.X509_VERIFY_PARAM_set1_host(param, host, len(host)) == 1
            )
        else:
            _SSL._openssl_assert(
                _SSL._lib.X509_VERIFY_PARAM_set1_ip(param, ip, len(ip)) == 1
            )
        if server.alpn_offers:
            conn.set_alpn_protos(list(server.alpn_offers))
        conn.set_connect_state()
        tls_start.ssl_conn = conn


    def reload(self) -> None:
        """Reload the port registry and clear the UDS cache (SIGHUP action).

        Policy is loaded per-request from disk, so a reload only needs to
        refresh the port map and drop cached lookups.
        """
        self._port_map = self._load_port_registry()
        self._cache.clear()

    def _load_port_registry(self) -> Dict[int, str]:
        """Load {port:int -> sandbox} from port-registry.json (tolerant keys).

        The file shape written by the Go port pool is:
            {"ports": {"<port>": {"sandbox": "<name>", "state": "active"|...}}}
        Only entries whose state is active (or unset) map a port to its sandbox;
        a "removing" tombstone is treated as not-yet-a-sandbox (fail closed).
        Keys may be JSON strings or ints. Any read/parse failure yields an empty
        map, so every port fails closed as an unknown sandbox until the registry
        is valid.
        """
        try:
            with open(self.port_registry_path, "r", encoding="utf-8") as fh:
                raw = json.load(fh)
        except (OSError, ValueError):
            return {}
        if not isinstance(raw, dict):
            return {}
        ports = raw.get("ports")
        if not isinstance(ports, dict):
            return {}
        out: Dict[int, str] = {}
        for k, entry in ports.items():
            try:
                port = int(k)
            except (ValueError, TypeError):
                continue
            name = self._entry_sandbox(entry)
            if name:
                out[port] = name
        return out

    @staticmethod
    def _entry_sandbox(entry: object) -> Optional[str]:
        """Extract the active sandbox name from a port-registry entry.

        Accepts the object form {"sandbox": "<name>", "state": "<state>"} and,
        defensively, a bare string. A "removing" tombstone yields None.
        """
        if isinstance(entry, str):
            return entry or None
        if isinstance(entry, dict):
            name = entry.get("sandbox")
            state = entry.get("state", "active")
            if isinstance(name, str) and name and state != "removing":
                return name
        return None

    # -- identity --

    def sandbox_for(self, flow: object) -> Optional[str]:
        """Resolve the sandbox name for a flow by its WireGuard listen port.

        Identity comes ONLY from flow.client_conn.proxy_mode.listen_port() —
        the cryptographically-bound listen port. The inner IP/sockname are
        never consulted (spoofable). An unknown or unreadable port returns
        None → the caller denies (fail closed).
        """
        port = self._listen_port(flow)
        if port is None:
            return None
        return self._port_map.get(port)

    @staticmethod
    def _listen_port(flow: object) -> Optional[int]:
        try:
            return int(flow.client_conn.proxy_mode.listen_port())
        except (AttributeError, TypeError, ValueError):
            return None

    # -- request hook --

    def request(self, flow: object) -> None:
        """mitmproxy request hook: identify, enforce policy, inject secrets."""
        try:
            self._handle_request(flow)
        except Exception as exc:  # noqa: BLE001 - never crash the proxy; fail closed
            self._log_warn("ppp: request handling failed, blocking: %r" % (exc,))
            self._block(flow, BLOCK_STATUS, "internal error")

    def _handle_request(self, flow: object) -> None:
        sandbox = self.sandbox_for(flow)
        req = flow.request
        host = req.pretty_host if hasattr(req, "pretty_host") else req.host

        if sandbox is None:
            # Unknown port → not a known sandbox → deny (fail closed).
            self._block(flow, BLOCK_STATUS, "unknown sandbox")
            self._deny_and_log(flow, "unknown", "")
            return

        port = getattr(req, "port", 0) or 0
        policy = load_policy_for(sandbox, self.data_dir, self.global_policy_path)
        decision, matched = policy.evaluate(host, port)
        rule_id = matched.id if matched is not None else ""

        if decision != "allow":
            self._block(flow, BLOCK_STATUS, "policy deny")
            self._deny_and_log(flow, sandbox, rule_id)
            return

        # Exfil-size gate (outbound) before any secret injection.
        if self._request_too_large(req):
            self._block(flow, TOO_LARGE_STATUS, "request too large")
            self._deny_and_log(flow, sandbox, rule_id)
            return

        # DNS-rebind guard: reject an allowed host that resolves to a
        # private/loopback/link-local/metadata IP. Fails CLOSED (code review
        # S5): a host that is not an IP literal but cannot be resolved is
        # blocked rather than injected, and ALL resolved records are checked —
        # any forbidden address blocks the request.
        if not self._host_is_allowed_by_rebind_guard(host):
            self._block(flow, REBIND_STATUS, "dns rebind blocked")
            self._deny_and_log(flow, sandbox, rule_id)
            return

        self._inject_secret(flow, sandbox, host)
        # An allowed flow is logged once, on response completion (so the log
        # line carries the real upstream status and bytes_in).

    def _host_is_allowed_by_rebind_guard(self, host: str) -> bool:
        """Return True if host is safe from a DNS-rebind standpoint.

        - An IP literal is checked directly.
        - A name is resolved; resolution failure fails closed (False).
        - Every resolved address must be non-forbidden; any private/loopback/
          link-local/metadata address fails closed.
        """
        try:
            ipaddress.ip_address(host)
            is_literal = True
        except ValueError:
            is_literal = False
        if is_literal:
            return not is_forbidden_ip(host)
        resolved = self._resolve(host)
        if not resolved:
            return False  # cannot resolve a name → fail closed
        if isinstance(resolved, str):  # tolerate a single-string resolver
            resolved = [resolved]
        return all(not is_forbidden_ip(ip) for ip in resolved)

    def _deny_and_log(self, flow: object, sandbox: str, rule_id: str) -> None:
        """Log a denied flow now and mark it so response() will not re-log it."""
        self._write_flow_log(self._flow_ctx(flow, sandbox), "deny", rule_id)
        self._logged_ids.add(id(flow))

    def _request_too_large(self, req: object) -> bool:
        content = getattr(req, "raw_content", None)
        if content is not None:
            return len(content) > MAX_REQUEST_BYTES
        length = self._header_content_length(req)
        return length is not None and length > MAX_REQUEST_BYTES

    @staticmethod
    def _header_content_length(msg: object) -> Optional[int]:
        try:
            raw = msg.headers.get("content-length")
        except AttributeError:
            return None
        if raw is None:
            return None
        try:
            return int(raw)
        except ValueError:
            return None

    # -- secret injection --

    def _inject_secret(self, flow: object, sandbox: str, host: str) -> None:
        """Query the UDS (cached) and apply the injection/substitutions.

        Custom substitutions run FIRST (over client-supplied headers), THEN the
        service credential is injected. This ordering ensures a custom
        substitution can never rewrite the just-injected credential header
        (code review S2) — the injected value is always the final, authoritative
        one. Client-supplied credential headers are stripped before injecting so
        the agent can never supply/override the key.
        """
        custom = self._cached_query({"service": "", "sandbox": sandbox, "host": host})
        self._apply_custom_response(flow, custom, skip=None)

        service = host_to_service(host)
        if service is not None:
            resp = self._cached_query({"service": service, "sandbox": sandbox, "host": host})
            self._apply_service_response(flow, resp)

    def _apply_service_response(self, flow: object, resp: Optional[dict]) -> None:
        if not resp or not resp.get("ok"):
            reason = (resp or {}).get("reason")
            if reason == "locked":
                self._log_warn("ppp: secret locked; no injection")
            return  # miss / locked / error → inject nothing
        header = resp.get("header")
        value = resp.get("value")
        if not isinstance(header, str) or not isinstance(value, str) or header == "":
            return
        headers = flow.request.headers
        # Strip any client-supplied credential headers first, then the exact
        # header we are about to set (case-insensitive), so the injected value
        # is the only one present.
        self._strip_credential_headers(headers)
        if header.lower() not in _STRIP_HEADERS and header in headers:
            del headers[header]
        headers[header] = value

    @staticmethod
    def _strip_credential_headers(headers: object) -> None:
        for name in _STRIP_HEADERS:
            try:
                if name in headers:
                    del headers[name]
            except (KeyError, AttributeError):
                continue

    def _apply_custom_response(
        self, flow: object, resp: Optional[dict], skip: Optional[set] = None
    ) -> None:
        if not resp or not resp.get("ok"):
            return
        subs = resp.get("substitutions")
        if not isinstance(subs, list) or not subs:
            return
        skip_lower = {s.lower() for s in skip} if skip else set()
        headers = flow.request.headers
        for name in list(headers.keys()):
            # Never rewrite a header we injected as a credential (defense in
            # depth; with custom-first ordering the injected header does not yet
            # exist, but this keeps the invariant explicit).
            if name.lower() in skip_lower:
                continue
            original = headers[name]
            replaced = original
            for sub in subs:
                if not isinstance(sub, dict):
                    continue
                placeholder = sub.get("placeholder")
                value = sub.get("value")
                if isinstance(placeholder, str) and placeholder and isinstance(value, str):
                    replaced = replaced.replace(placeholder, value)
            if replaced != original:
                headers[name] = replaced

    def _cached_query(self, request: Dict[str, str]) -> Optional[dict]:
        """Return a cached UDS response for (service, sandbox, host) or query.

        The 60s cache keys on the exact lookup tuple; on a miss it performs the
        UDS query and caches the result. SIGHUP (reload) clears the cache.
        """
        key = (request.get("service", ""), request.get("sandbox", ""), request.get("host", ""))
        now = time.monotonic()
        cached = self._cache.get(key)
        if cached is not None and cached[1] > now:
            return cached[0]
        resp = uds_query(self.socket_path, request)
        if resp is not None:
            self._cache[key] = (resp, now + CACHE_TTL_SECONDS)
        return resp

    # -- response hook --

    def response(self, flow: object) -> None:
        """mitmproxy response hook: exfil-size gate (inbound) + flow log."""
        try:
            self._handle_response(flow)
        except Exception as exc:  # noqa: BLE001 - never crash the proxy
            self._log_warn("ppp: response handling failed: %r" % (exc,))

    def _handle_response(self, flow: object) -> None:
        resp = getattr(flow, "response", None)
        if resp is None:
            return
        # A flow denied in the request hook was already blocked-and-logged;
        # mitmproxy still fires the response hook for that synthetic response,
        # so skip flows we marked as already logged (avoid a double log line).
        if id(flow) in self._logged_ids:
            self._logged_ids.discard(id(flow))
            return
        # Inbound exfil-size gate: replace an oversized response with 413.
        content = getattr(resp, "raw_content", None)
        sandbox = self.sandbox_for(flow) or "unknown"
        if content is not None and len(content) > MAX_RESPONSE_BYTES:
            self._block(flow, TOO_LARGE_STATUS, "response too large")
            self._write_flow_log(self._flow_ctx(flow, sandbox), "deny", "")
            return
        self._write_flow_log(self._flow_ctx(flow, sandbox), "allow", "")

    # -- helpers --

    def _flow_ctx(self, flow: object, sandbox: str) -> Dict[str, object]:
        """Extract the non-sensitive fields needed for a flow log line."""
        req = getattr(flow, "request", None)
        resp = getattr(flow, "response", None)
        host = ""
        method = ""
        bytes_out = 0
        if req is not None:
            host = getattr(req, "pretty_host", None) or getattr(req, "host", "") or ""
            method = getattr(req, "method", "") or ""
            raw = getattr(req, "raw_content", None)
            bytes_out = len(raw) if raw is not None else 0
        status = getattr(resp, "status_code", None) if resp is not None else None
        bytes_in = 0
        if resp is not None:
            raw_r = getattr(resp, "raw_content", None)
            bytes_in = len(raw_r) if raw_r is not None else 0
        return {
            "sandbox": sandbox,
            "host": host,
            "method": method,
            "status": status,
            "bytes_out": bytes_out,
            "bytes_in": bytes_in,
        }

    def _write_flow_log(self, fctx: Dict[str, object], decision: str, matched_rule: str) -> None:
        record = build_flow_log(
            sandbox=str(fctx["sandbox"]),
            host=str(fctx["host"]),
            method=str(fctx["method"]),
            status=fctx["status"],  # type: ignore[arg-type]
            decision=decision,
            matched_rule=matched_rule,
            bytes_out=int(fctx["bytes_out"]),  # type: ignore[arg-type]
            bytes_in=int(fctx["bytes_in"]),  # type: ignore[arg-type]
        )
        line = json.dumps(record, separators=(",", ":")) + "\n"
        try:
            with open(self.flow_log_path, "a", encoding="utf-8") as fh:
                fh.write(line)
        except OSError as exc:
            self._log_warn("ppp: flow log write failed: %r" % (exc,))

    @staticmethod
    def _block(flow: object, status: int, reason: str) -> None:
        """Set flow.response to a blocking response so the request never leaves.

        Uses the real mitmproxy http.Response.make when available; falls back
        to a lightweight stand-in under unit tests where http is not imported.
        """
        if http is not None:
            flow.response = http.Response.make(status, reason.encode("utf-8"), {"Content-Type": "text/plain"})
        else:  # pragma: no cover - real mitmproxy path is preferred
            flow.response = _FakeResponse(status, reason)

    @staticmethod
    def _log_warn(msg: str) -> None:
        # mitmproxy 12.x uses the stdlib logging module (the legacy ctx.log was
        # removed), so warnings go through a module logger. Never include a
        # secret value in a message passed here.
        _logger.warning(msg)


class _FakeResponse:
    """Minimal response stand-in used only when mitmproxy.http is unavailable.

    Real deployments always have mitmproxy.http, so http.Response.make is used
    there; this exists so the addon's block path is exercisable in pure unit
    tests without importing mitmproxy.
    """

    def __init__(self, status_code: int, text: str) -> None:
        self.status_code = status_code
        self.text = text
        self.content = text.encode("utf-8")
        self.raw_content = self.content


addons = [PppAddon()]
