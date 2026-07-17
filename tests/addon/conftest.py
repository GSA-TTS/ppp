"""pytest bootstrap: make assets/addon.py importable and expose fakes."""

import os
import sys

_ASSETS = os.path.join(os.path.dirname(__file__), "..", "..", "assets")
sys.path.insert(0, os.path.abspath(_ASSETS))
