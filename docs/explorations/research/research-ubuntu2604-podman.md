# Research: podman in Ubuntu 26.04 LTS ("Resolute Numbat")

_Read-only investigation for the `ppp` devcontainer. No repo changes made._
_As-of host date: mid-July 2026._

## Bottom line

**Ubuntu 26.04 LTS ("Resolute Numbat") ships podman `5.7.0+ds2-3build1` — the FULL
`podman` client from `apt` (universe) — NOT 6.x.** Upstream podman 6.0.x exists (6.0.1
released ~June 2026) but has NOT been packaged in Debian or Ubuntu as of mid-2026, so it
did not make it into the 26.04 GA archive. If you need podman 6.x on 26.04 you'd have to
use an external repo (e.g. Kubic/OpenSUSE OBS) or build from source — it is not in apt.

- **Codename / version:** 26.04 = **Resolute Numbat** = codename **`resolute`**. It is
  **released** (GA April 2026). Launchpad lists it as **"Current Stable Release"**; the
  in-development series is now 26.10 "Stonking".
- **podman in `resolute`:** `5.7.0+ds2-3build1` (universe), uploaded 2026-02-23.
- Full package set present: `podman`, `podman-remote`, `podman-docker`, `podman-dbgsym`,
  built for amd64/amd64v3/arm64/armhf/ppc64el/riscv64/s390x.

## 1. Release status of Ubuntu 26.04

- 26.04 = **Resolute Numbat**, delivered **April 2026** (Launchpad: "The Ubuntu release
  that will be delivered in April 2026, designated 26.04.").
- Launchpad timeline marks **Resolute (26.04) = "Current Stable Release"**; its successor
  **Stonking (26.10) = "Active Development"**. So 26.04 is **released/GA**, not in flux.
- Because it is GA, the archive is frozen at release versions (further changes land in
  `resolute-updates` / `-security`, but no podman SRU was observed).
- Evidence: https://launchpad.net/ubuntu/+series
- Ubuntu wiki Releases page last edited 2026-06-16 (confirms mid-2026 currency):
  https://wiki.ubuntu.com/Releases

## 2. podman in 26.04 "resolute" — exact version

- **`podman 5.7.0+ds2-3build1`** (universe), uploaded **2026-02-23**, Medium urgency.
- Source page: https://launchpad.net/ubuntu/resolute/+source/podman
- Binaries built for: amd64, amd64v3, arm64, armhf, ppc64el, riscv64, s390x.
- Build-deps confirm it's the full daemon/client (buildah >=1.42, containers-common >=0.66,
  gvisor-tap-vsock >=0.7.4, etc.), and it produces `podman`, `podman-remote`,
  `podman-docker` — i.e. the FULL podman client, not a stub.
- `packages.ubuntu.com/resolute/podman` timed out during research; Launchpad is the
  authoritative source and gave the exact version above.

## 3. Full client confirmation

Yes — Ubuntu ships the complete `podman` package (plus `podman-remote` and
`podman-docker`). It is the real client/engine, version **5.7.0+ds2-3build1**.

## 4. Comparison — 25.10 "questing" and the trend

- **25.10 (questing):** `podman 5.4.2+ds1-2` (universe), uploaded 2025-07-08.
  https://launchpad.net/ubuntu/questing/+source/podman
- Trend: 25.10 = 5.4.2  →  26.04 = 5.7.0. Ubuntu moved forward within the **5.x** line
  but did **NOT** reach 6.x by 26.04. Ubuntu tracks Debian, and Debian had not packaged
  6.x in time (see §5).

## 5. Debian — is 6.x anywhere that would flow into Ubuntu 26.04?

Ubuntu pulls podman from Debian. Debian package tracker (as of 2026-07-16):
https://tracker.debian.org/pkg/podman

- **stable (trixie):** `5.4.2+ds1-2`
- **testing (forky):** `5.8.3+ds1-1`
- **unstable (sid):** `5.8.3+ds1-1`
- **experimental:** last podman upload to experimental was `5.8.1+ds1-1` (2026-03-13) —
  still 5.x, no 6.x.
- Debian tracker explicitly flags: **"A new upstream version is available: 6.0.1 ... you
  should consider packaging it"** (created 2026-06-29). => Upstream podman **6.0.1**
  exists, but Debian has **NOT** packaged any 6.x anywhere (not sid, not experimental).
- `madison` confirms: bullseye 3.0.1, bookworm 4.3.1, trixie 5.4.2, forky 5.8.3, sid 5.8.3.
  No 6.x row anywhere.
- Because Debian had no 6.x, and Ubuntu 26.04 froze at `5.7.0+ds2-3build1` (an even older
  point than Debian testing's 5.8.3), **6.x could not have flowed into 26.04**.

Note: podman 6.0.0 fixes CVE-2026-57231; the 5.7.0 in 26.04 is below the 5.8.4 fix line,
so the 26.04 archive podman is potentially affected until an Ubuntu SRU lands.

## 6. Go and Python in 26.04 "resolute"

- **Go:** `golang-defaults 2:1.26~1` (main), uploaded 2026-02-22 → default `golang-go`
  is **Go 1.26**. Comfortably satisfies the Go 1.22+ requirement.
  https://launchpad.net/ubuntu/resolute/+source/golang-defaults
- **Python:** `python3-defaults 3.14.3-0ubuntu2` (main), uploaded 2026-03-21 → default
  `python3` is **Python 3.14**. Satisfies the Python >=3.12 requirement for
  mitmproxy 12.2.3. (The `-dbg` metapackage even references 3.14; 3.13 is also present.)
  https://launchpad.net/ubuntu/resolute/+source/python3-defaults

## Evidence URLs (summary)

- Ubuntu series/status: https://launchpad.net/ubuntu/+series
- podman @ resolute (26.04): https://launchpad.net/ubuntu/resolute/+source/podman
- podman @ questing (25.10): https://launchpad.net/ubuntu/questing/+source/podman
- Debian podman tracker: https://tracker.debian.org/pkg/podman
- Debian madison (podman versions): https://qa.debian.org/madison.php?package=podman
- Go @ resolute: https://launchpad.net/ubuntu/resolute/+source/golang-defaults
- Python @ resolute: https://launchpad.net/ubuntu/resolute/+source/python3-defaults

## Implications for the `ppp` devcontainer

- If the devcontainer targets 26.04 `apt`, plan for **podman 5.7.0**, not 6.x. Any code
  assuming a podman 6.x CLI/API surface will need a build-from-source, Kubic/OBS repo, or
  the podman-provided static binary instead of `apt`.
- Go 1.26 and Python 3.14 from apt both exceed the project's minimums (Go 1.22+, Py 3.12+).
