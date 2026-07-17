"""pytest bootstrap: make assets/addon.py importable and expose fakes."""

import json
import os
import sys

_ASSETS = os.path.join(os.path.dirname(__file__), "..", "..", "assets")
sys.path.insert(0, os.path.abspath(_ASSETS))


def port_registry(mapping):
    """Render a port-registry.json body in the real nested shape written by the
    Go port pool: {"ports": {"<port>": {"sandbox": "<name>", "state": "active"}}}.

    Accepts {port(str|int) -> sandbox_name} for terse tests.
    """
    ports = {
        str(port): {"sandbox": name, "state": "active"}
        for port, name in mapping.items()
    }
    return json.dumps({"ports": ports})

