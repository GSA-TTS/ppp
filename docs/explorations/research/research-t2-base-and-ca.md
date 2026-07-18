# Research T2 — Devcontainer base image (podman 6.0.x) & Zscaler Root CA

Read-only investigation for the `ppp` devcontainer. No repo changes made.
Date: 2026-07-16. Host: darwin. Evidence gathered via Launchpad API,
packages.debian.org, Fedora Bodhi/COPR APIs, openSUSE OBS, GitHub API, and
`gh` code search.

---

## QUESTION A — Linux base image that installs the FULL podman **6.0.x** client from a package manager

**Goal:** a devcontainer that mirrors the host's podman **6.0.1** CLI contract
(`podman --version`, `podman machine --help` must work). MicroVMs need NOT boot
in-container. We want the full `podman` client from a package manager, not the
upstream static `podman-remote` binary.

### A.1 Ubuntu (packages via Launchpad `+source/libpod` / `+package/podman`)

| Release | Codename | podman version in archive | Evidence |
|---|---|---|---|
| 24.04 LTS | noble | **4.9.3+ds1-1build2** (security update `4.9.3+ds1-1ubuntu0.1`) | `launchpad.net/ubuntu/noble/+source/libpod` |
| 24.10 | oracular | **4.9.4+ds1-1** (base `4.9.3+ds1-1build2`) | `launchpad.net/ubuntu/oracular/+source/libpod` |
| 25.04 | plucky | **5.0.3+ds1-5ubuntu1** | `launchpad.net/ubuntu/plucky/+source/libpod` |
| 25.10 | questing | **5.4.2+ds1-2** (`+package/podman`) | `launchpad.net/ubuntu/questing/+package/podman` |

**No supported Ubuntu release ships podman 6.0.x.** The newest (questing / 25.10)
is 5.4.2. LTS noble is stuck at 4.9.3.

### A.2 Debian (packages.debian.org)

| Suite | podman version | Evidence |
|---|---|---|
| bookworm (12) | **4.3.1+ds1-8+deb12u1** | `packages.debian.org/bookworm/podman` |
| trixie (13, current stable) | **5.4.2+ds1-2** | `packages.debian.org/trixie/podman` |
| forky (14, testing) | **5.8.3+ds1-1** | `packages.debian.org/forky/podman` |
| sid (unstable) | **5.8.3+ds1-1** | `packages.debian.org/sid/podman` |

**No Debian suite ships podman 6.0.x either** — sid/forky top out at 5.8.3.
No `trixie-backports` podman exists yet.

### A.3 Fedora (Bodhi update system + COPR)

| Release | State | podman (stable) | Go | Python |
|---|---|---|---|---|
| F42 | maintenance | 5.8.2 | — | — |
| F43 | **current** | **5.8.4-1.fc43** | golang 1.25.x | python3.13 3.13.14, python3.12 3.12.13 both packaged |
| F44 | **current** (next) | **5.8.4-1.fc44** | golang 1.26.x | python3.13 3.13.14 |
| F45 | pending (branched) | **6.0.0-1.fc45**, then **6.0.1-1.fc45** | — | — |
| ELN (RHEL-next) | — | **6.0.1-1.eln158** | — | — |

Evidence: `bodhi.fedoraproject.org/updates/?packages=podman` (JSON API).

**Key finding:** podman **6.0.1** IS packaged for **Fedora 45** (`podman-6.0.1-1.fc45`)
and ELN (`podman-6.0.1-1.eln158`) — both as **stable Bodhi updates**. But F45 is
still in `pending`/branched state (not yet GA), and the current GA Fedoras
(F43/F44) are on 5.8.4. So `dnf` on a released Fedora does not yet give 6.0.1.

**COPR `rhcontainerbot/podman-next`** (official upstream nightly RPMs) currently
builds **`102:6.1.0~dev...main`** for `fedora-43/44/rawhide`, `centos-stream-9/10`,
`epel-9/10`. This is a *development* build (6.1.0-dev), explicitly "CANNOT be
recommended for production," so it does not pin to 6.0.1.

### A.4 Kubic / OBS `devel:kubic:libcontainers:stable`

- **Deprecated / frozen.** The OBS project root still returns HTTP 200, but its
  per-distro subdirs stop at old releases: `xUbuntu_18.04 … xUbuntu_22.04`,
  `Debian_10/11/12` + `Debian_Testing`/`Debian_Unstable`, `Fedora_36/37/38`,
  `CentOS_7/8/9_Stream`.
- The subdirs that matter here **do not exist** — both return **HTTP 404**:
  - `.../stable/xUbuntu_24.04/` → 404
  - `.../stable/Debian_13/` → 404
- There is **no xUbuntu_24.04 / 25.10 or Debian_13 tree**, so this repo cannot
  provide podman 6.0.x (or even 5.x) for a modern Ubuntu/Debian base. It was
  superseded by distro-native packaging and is effectively unmaintained for
  2024+ targets. **Do not use it.**

### A.5 Upstream static `podman-remote` (for contrast)

`gh api repos/containers/podman/releases` confirms upstream tag **`v6.0.1`**
(published 2026-07-08) ships `podman-remote-static-linux_amd64.tar.gz` and
`_arm64`. This is the *remote-only* client (talks to a machine/socket); it is
NOT the full `podman` binary with the local engine. It would satisfy
`podman --version` and `podman machine --help`, but it is the very thing we were
asked to avoid unless nothing else works.

### A.6 RECOMMENDATION (Question A)

**No `apt`/`dnf` archive on a *released* distro currently ships podman 6.0.x.**
The only package-manager routes to a real 6.0.x full client today are:

1. **`fedora:rawhide` (F46) or `registry.fedoraproject.org/fedora:41`+`updates-testing`** — moving target, not pinnable to 6.0.1.
2. **Fedora 45 once GA**, or an **F45-based image now** (e.g. `quay.io/fedora/fedora:45`) with `dnf install podman` → **`podman-6.0.1-1.fc45`**. F45 is branched/pending, so this is a near-term but not-yet-stable option.

**Recommended base image + method — pick by tolerance for pre-release:**

- **Preferred, gets exactly 6.0.1 from a package manager:**
  **`quay.io/fedora/fedora:45`** (or `registry.fedoraproject.org/fedora:45`),
  then `dnf -y install podman` → **podman 6.0.1-1.fc45**. This is a `dnf`-based
  devcontainer. Go (golang 1.25+) and Python (3.12/3.13) are both in the Fedora
  repos (`dnf install golang python3.12` — satisfies Go 1.22+ and Python ≥3.12
  for mitmproxy 12.2.3), plus `@development-tools`/`gcc`/`make` for build tooling.
  Caveat: F45 is branched (pending GA); treat as "current-but-fresh." Pin the
  exact NEVRA (`podman-6.0.1-1.fc45`) in the Dockerfile so builds are reproducible.

- **If you refuse a pre-GA Fedora and insist on package-manager-only:** there is
  **no way to get 6.0.x** — the newest packaged full client anywhere on a GA
  distro is **5.8.4** (Fedora 43/44) / **5.8.3** (Debian sid). If mirroring the
  *6.0.x CLI contract* (flags/`machine` subcommands) is what actually matters and
  5.8.x's contract is close enough, use **`fedora:43`/`44`** → podman 5.8.4.

- **If you must have 6.0.1 *exactly* and want it stable/reproducible today:** the
  honest answer is that the **upstream static `podman-remote` v6.0.1 binary is the
  only pinnable source of a 6.0.1 client right now** for a non-Fedora base. It is
  remote-only (no local engine), but for a devcontainer that only needs the CLI
  contract (`podman --version`, `podman machine --help`) and won't boot microVMs,
  that is functionally sufficient. Verify its published checksum. Use this only
  if the Fedora-45 route is unacceptable.

**Bottom line:** to get the **full podman 6.0.1 client from a package manager**,
the single best base is **Fedora 45** (`dnf install podman` → `podman-6.0.1-1.fc45`).
Go 1.25+, Python 3.12/3.13, and build tooling are all trivially `dnf`-installable
on it. Every Ubuntu/Debian archive and the dead Kubic OBS repo fall short (≤5.8.x
/ no modern trees).

---

## QUESTION B — Generic PUBLIC Zscaler Root CA (as used by GSA)

### B.1 Identification & cross-verification

`gh api search/code -f q='filename:ZscalerRootCA.crt'` → 22 hits. Fetched raw
copies from several **independent** public repos and fingerprinted with openssl.

**The generic public "Zscaler Root CA" (2014 generation) — the one GSA uses:**

- **Subject / Issuer (self-signed root):**
  `C=US, ST=California, L=San Jose, O=Zscaler Inc., OU=Zscaler Inc., CN=Zscaler Root CA, emailAddress=support@zscaler.com`
- **SHA-256 fingerprint:**
  `04:F6:1F:1D:13:AA:E1:D1:65:73:DC:2C:37:F7:96:FD:F4:AC:97:71:3A:69:59:EB:B1:1D:24:73:95:8B:1A:53`
  (hex: `04F61F1D13AAE1D16573DC2C37F796FDF4AC97713A6959EBB11D2473958B1A53`)
- **Serial:** `DBBE982D89B77B93`
- **Validity:** notBefore `Dec 19 00:27:55 2014 GMT` — notAfter `May 6 00:27:55 2042 GMT`
- **Key:** RSA 2048

**Cross-check — byte-for-byte identical DER across all independent copies:**

| Source repo | Path | DER SHA-256 |
|---|---|---|
| `zscaler/terraform-aws-cloud-connector-modules` (**Zscaler's own repo**) | `.../ZscalerRootCA.crt` | `04f61f…1a53` |
| `erwinkramer/bank-api` | `.certs/ZscalerRootCA.crt` | `04f61f…1a53` |
| `bschofield-va/fresnel` | `certs/ZscalerRootCA.crt` | `04f61f…1a53` |
| `tstockdale/streamlit` | `ZscalerRootCA.crt` | `04f61f…1a53` |
| **`GSA-TTS/agentic-coding-patterns`** | `integrations/isolation/acq-kits/zscaler-ca-certificate/files/home/zscaler-ca.crt` | `04f61f…1a53` |

All four independent copies + Zscaler's own repo agree exactly, and the copy
**GSA-TTS itself vendors** in its `zscaler-ca-certificate` acq mixin kit is
**byte-identical** (1732-byte PEM `diff` = no difference). This is definitively
the generic public root GSA uses.

### B.2 There ARE two distinct "Zscaler Root CA" certs in the wild

`gh api search/code -f q='"Zscaler Root CA" extension:crt'` surfaced a **newer
2025 generation** vendored as `ZscalerRootCertificate-2048-SHA256-Feb2025.crt`
(repos `grisottom/my-inji-stack`, `Jozef833/nix-config`):

- **SHA-256:** `B5:CA:18:B6:68:D1:D8:5B:51:E9:64:EE:06:40:7B:0D:88:E4:8D:F6:DA:AB:D5:9E:93:F5:B7:4C:29:22:99:97`
- Same subject CN "Zscaler Root CA", same serial `DBBE982D89B77B93`
- **Validity:** notBefore `Feb 2 16:38:20 2025 GMT` — notAfter `Jun 20 16:38:20 2052 GMT`

**Flag:** two different certs share the CN/serial but have different keys,
validity, and fingerprints. The **2014 root (`04F61F…1A53`)** is the long-standing
**generic public** one vendored everywhere and — decisively — the one **GSA-TTS
ships in its own tooling**. The 2025 root (`B5CA18…9997`) is a newer generation
seen far less often. **Use the 2014 root as the GSA/generic public cert; pin its
fingerprint. Optionally trust both** for forward-compat.

### B.3 Reproducible obtain: **vendor, don't fetch** (recommended)

A public root cert is stable (immutable until 2042). Vendor it and verify by
SHA-256 at build:

```
.devcontainer/certs/ZscalerRootCA.crt        # the 2014 PEM (1732 bytes)
```

Dockerfile (Debian/Ubuntu base):
```dockerfile
COPY .devcontainer/certs/ZscalerRootCA.crt /usr/local/share/ca-certificates/ZscalerRootCA.crt
RUN echo "04F61F1D13AAE1D16573DC2C37F796FDF4AC97713A6959EBB11D2473958B1A53  /usr/local/share/ca-certificates/ZscalerRootCA.crt" \
      | sha256sum -c - \
    && update-ca-certificates
```

Fedora/`dnf` base equivalent:
```dockerfile
COPY .devcontainer/certs/ZscalerRootCA.crt /etc/pki/ca-trust/source/anchors/ZscalerRootCA.crt
RUN printf '%s  %s\n' 04F61F1D13AAE1D16573DC2C37F796FDF4AC97713A6959EBB11D2473958B1A53 \
      /etc/pki/ca-trust/source/anchors/ZscalerRootCA.crt | sha256sum -c - \
    && update-ca-trust
```

**Pin against:** `04F61F1D13AAE1D16573DC2C37F796FDF4AC97713A6959EBB11D2473958B1A53`.
Vendoring is preferred over fetch-at-build: no egress, no dependency on a
Zscaler URL, reproducible, and it matches exactly what GSA-TTS's own acq kit does
(their `scripts/verify` fingerprints the installed cert against the embedded PEM).

### B.4 Zero-action auto-enable that doesn't break external contributors

Three options, with the tradeoff:

1. **Unconditional install (RECOMMENDED).** Always `COPY` + verify + install the
   pinned public root into the trust store at build. **Requires ZERO action from a
   GSA/Zscaler user** and is **harmless on non-inspected networks**: adding an
   extra trusted root only means TLS chains that *actually terminate at this
   specific Zscaler interceptor* are trusted. On a normal network nothing routes
   through that interceptor, so the added root is simply never exercised — it
   cannot silently MITM a connection that isn't already being intercepted by that
   exact CA's private key (which only Zscaler/the org proxy holds).

   **Is it acceptable to bake a well-known public interception root into a PUBLIC
   repo's image?** Yes, for a dev image, with eyes open. The cert is already
   public (vendored in hundreds of repos incl. Zscaler's own and GSA-TTS's).
   Trusting it does **not** grant anyone new power: only a party already holding
   this CA's private key (the org's Zscaler tenant) can mint certs it validates,
   and only for traffic they already intercept. It does **not** weaken trust for
   ordinary internet TLS. The residual concern is purely that a dev *could* be
   MITM'd by *that* interceptor without warning — acceptable in a dev sandbox,
   and exactly the posture GSA-TTS already ships.

2. **Build-time auto-detect (probe interception), conditional trust.** In an
   entrypoint/build step, inspect the root of the presented chain for `github.com`
   and only install if it's Zscaler:
   ```sh
   root_cn=$(echo | openssl s_client -connect github.com:443 -showcerts 2>/dev/null \
     | openssl x509 -noout -issuer 2>/dev/null)
   case "$root_cn" in *"Zscaler"*) install_zscaler_ca ;; esac
   ```
   Cleaner in theory, but adds a network probe to the build, is flaky behind
   proxies/offline builds, and gives no benefit over option 1 (which is inert
   off-Zscaler). More moving parts, more ways to break a non-Zscaler build.

3. **Fetch-at-build.** Rejected — needs egress and a stable URL; a public root
   doesn't change, so vendoring is strictly better.

**Concrete recommendation:** Do **option 1** — vendor
`.devcontainer/certs/ZscalerRootCA.crt` (the 2014 root), SHA-256-verify against
`04F61F…1A53` at build, and unconditionally `update-ca-certificates`/`update-ca-trust`.
This is zero-config for GSA/Zscaler users, inert and safe for external
contributors on normal networks, reproducible, and mirrors GSA-TTS's own
`zscaler-ca-certificate` kit. (Optionally also vendor the 2025 root
`B5CA18…9997` for forward-compatibility; same install pattern.)

---

## Verified PEM (2014 generic public Zscaler Root CA — pin this)

SHA-256: `04F61F1D13AAE1D16573DC2C37F796FDF4AC97713A6959EBB11D2473958B1A53`

```
-----BEGIN CERTIFICATE-----
MIIE0zCCA7ugAwIBAgIJANu+mC2Jt3uTMA0GCSqGSIb3DQEBCwUAMIGhMQswCQYD
VQQGEwJVUzETMBEGA1UECBMKQ2FsaWZvcm5pYTERMA8GA1UEBxMIU2FuIEpvc2Ux
FTATBgNVBAoTDFpzY2FsZXIgSW5jLjEVMBMGA1UECxMMWnNjYWxlciBJbmMuMRgw
FgYDVQQDEw9ac2NhbGVyIFJvb3QgQ0ExIjAgBgkqhkiG9w0BCQEWE3N1cHBvcnRA
enNjYWxlci5jb20wHhcNMTQxMjE5MDAyNzU1WhcNNDIwNTA2MDAyNzU1WjCBoTEL
MAkGA1UEBhMCVVMxEzARBgNVBAgTCkNhbGlmb3JuaWExETAPBgNVBAcTCFNhbiBK
b3NlMRUwEwYDVQQKEwxac2NhbGVyIEluYy4xFTATBgNVBAsTDFpzY2FsZXIgSW5j
LjEYMBYGA1UEAxMPWnNjYWxlciBSb290IENBMSIwIAYJKoZIhvcNAQkBFhNzdXBw
b3J0QHpzY2FsZXIuY29tMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA
qT7STSxZRTgEFFf6doHajSc1vk5jmzmM6BWuOo044EsaTc9eVEV/HjH/1DWzZtcr
fTj+ni205apMTlKBW3UYR+lyLHQ9FoZiDXYXK8poKSV5+Tm0Vls/5Kb8mkhVVqv7
LgYEmvEY7HPY+i1nEGZCa46ZXCOohJ0mBEtB9JVlpDIO+nN0hUMAYYdZ1KZWCMNf
5J/aTZiShsorN2A38iSOhdd+mcRM4iNL3gsLu99XhKnRqKoHeH83lVdfu1XBeoQz
z5V6gA3kbRvhDwoIlTBeMa5l4yRdJAfdpkbFzqiwSgNdhbxTHnYYorDzKfr2rEFM
dsMU0DHdeAZf711+1CunuQIDAQABo4IBCjCCAQYwHQYDVR0OBBYEFLm33UrNww4M
hp1d3+wcBGnFTpjfMIHWBgNVHSMEgc4wgcuAFLm33UrNww4Mhp1d3+wcBGnFTpjf
oYGnpIGkMIGhMQswCQYDVQQGEwJVUzETMBEGA1UECBMKQ2FsaWZvcm5pYTERMA8G
A1UEBxMIU2FuIEpvc2UxFTATBgNVBAoTDFpzY2FsZXIgSW5jLjEVMBMGA1UECxMM
WnNjYWxlciBJbmMuMRgwFgYDVQQDEw9ac2NhbGVyIFJvb3QgQ0ExIjAgBgkqhkiG
9w0BCQEWE3N1cHBvcnRAenNjYWxlci5jb22CCQDbvpgtibd7kzAMBgNVHRMEBTAD
AQH/MA0GCSqGSIb3DQEBCwUAA4IBAQAw0NdJh8w3NsJu4KHuVZUrmZgIohnTm0j+
RTmYQ9IKA/pvxAcA6K1i/LO+Bt+tCX+C0yxqB8qzuo+4vAzoY5JEBhyhBhf1uK+P
/WVWFZN/+hTgpSbZgzUEnWQG2gOVd24msex+0Sr7hyr9vn6OueH+jj+vCMiAm5+u
kd7lLvJsBu3AO3jGWVLyPkS3i6Gf+rwAp1OsRrv3WnbkYcFf9xjuaf4z0hRCrLN2
xFNjavxrHmsH8jPHVvgc1VD0Opja0l/BRVauTrUaoW6tE+wFG5rEcPGS80jjHK4S
pB5iDj2mUZH1T8lzYtuZy0ZPirxmtsk3135+CKNa2OCAhhFjE0xd
-----END CERTIFICATE-----
```
