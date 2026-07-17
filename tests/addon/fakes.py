"""Shared fakes for the ppp addon test suite.

These stand in for the mitmproxy flow object and the Go UDS parent so the
addon can be exercised without a live mitmproxy runtime, real secrets, or a
real socket to the Go server. A tiny in-process fake UDS server is provided for
the injection/cache tests because a real socket exercises the addon's transport
path more faithfully than mocking ``socket``.
"""

from __future__ import annotations

import json
import socket
import threading
from typing import Callable, Dict, List, Optional


class FakeHeaders:
    """A case-insensitive header multimap mirroring mitmproxy.http.Headers.

    Supports the operations the addon uses: ``in``, ``__getitem__``,
    ``__setitem__``, ``__delitem__``, ``get``, and ``keys``. Keys compare
    case-insensitively; the last-set spelling of a name is preserved.
    """

    def __init__(self, initial: Optional[Dict[str, str]] = None) -> None:
        self._store: Dict[str, str] = {}  # lower-name -> value
        self._names: Dict[str, str] = {}  # lower-name -> display name
        for k, v in (initial or {}).items():
            self[k] = v

    def __contains__(self, name: str) -> bool:
        return name.lower() in self._store

    def __getitem__(self, name: str) -> str:
        return self._store[name.lower()]

    def __setitem__(self, name: str, value: str) -> None:
        lname = name.lower()
        self._store[lname] = value
        self._names[lname] = name

    def __delitem__(self, name: str) -> None:
        lname = name.lower()
        del self._store[lname]
        self._names.pop(lname, None)

    def get(self, name: str, default: Optional[str] = None) -> Optional[str]:
        return self._store.get(name.lower(), default)

    def keys(self) -> List[str]:
        return [self._names[k] for k in self._store]


class FakeRequest:
    def __init__(
        self,
        host: str,
        method: str = "GET",
        port: int = 443,
        headers: Optional[Dict[str, str]] = None,
        content: bytes = b"",
    ) -> None:
        self.host = host
        self.pretty_host = host
        self.method = method
        self.port = port
        self.headers = FakeHeaders(headers)
        self.raw_content = content


class FakeResponse:
    def __init__(self, status_code: int = 200, content: bytes = b"") -> None:
        self.status_code = status_code
        self.raw_content = content
        self.content = content


class FakeProxyMode:
    def __init__(self, listen_port: int) -> None:
        self._port = listen_port

    def listen_port(self) -> int:
        return self._port


class FakeClientConn:
    def __init__(self, listen_port: int) -> None:
        self.proxy_mode = FakeProxyMode(listen_port)


class FakeClient:
    """Inner-connection view that the addon MUST NOT use for identity.

    sockname/address are set to a DIFFERENT sandbox's coordinates in the
    identity tests to prove identity comes only from the listen port.
    """

    def __init__(self, address: object = None, sockname: object = None) -> None:
        self.address = address
        self.sockname = sockname


class FakeFlow:
    def __init__(
        self,
        listen_port: int,
        request: FakeRequest,
        response: Optional[FakeResponse] = None,
        client: Optional[FakeClient] = None,
    ) -> None:
        self.client_conn = FakeClientConn(listen_port)
        self.request = request
        self.response = response
        self.client = client or FakeClient()


class FakeUdsServer:
    """A tiny newline-delimited-JSON UDS server standing in for the Go parent.

    It replies with whatever ``handler(request_dict)`` returns for each
    connection (one request/response per connection, matching the T8 contract)
    and counts how many requests it served so cache behavior can be asserted.
    """

    def __init__(self, socket_path: str, handler: Callable[[dict], dict]) -> None:
        self.socket_path = socket_path
        self._handler = handler
        self.request_count = 0
        self.requests: List[dict] = []
        self._sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self._sock.bind(socket_path)
        self._sock.listen(8)
        self._stop = False
        self._thread = threading.Thread(target=self._serve, daemon=True)
        self._thread.start()

    def _serve(self) -> None:
        while not self._stop:
            try:
                conn, _ = self._sock.accept()
            except OSError:
                return
            with conn:
                buf = bytearray()
                while b"\n" not in buf:
                    chunk = conn.recv(4096)
                    if not chunk:
                        break
                    buf.extend(chunk)
                raw, _, _ = bytes(buf).partition(b"\n")
                if not raw:
                    continue
                try:
                    req = json.loads(raw.decode("utf-8"))
                except ValueError:
                    continue
                self.request_count += 1
                self.requests.append(req)
                resp = self._handler(req)
                conn.sendall((json.dumps(resp) + "\n").encode("utf-8"))

    def close(self) -> None:
        self._stop = True
        try:
            self._sock.close()
        except OSError:
            pass
