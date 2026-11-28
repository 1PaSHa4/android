# Experimental Xray Android bridge

This is not a production VPN or a release APK. The normal Android app still uses its original direct transport. The isolated native library is not yet linked into Kotlin.

- Xray release v26.11.9 pinned by commit.
- TCP and experimental UDP via VLESS/REALITY; physical connections limited to the selected endpoint.
- UDP payloads above 2048 bytes are rejected; write deadlines are not implemented.
- Endpoint IP must be supplied explicitly. Trusted DNS bootstrap, provider profile import and encrypted credential storage remain unimplemented.
- Endpoint probes establish TCP only; they do not authenticate the VPN server. Android's always-on/lockdown, app exclusions, DNS/IPv6 leak checks and live two-network tests are still required.
- CI runs the Go smoke tests and attempts to build an arm64 AAR. An AAR is not an installable APK.

Do not supply real credentials to CI, GitHub issues, logs, or this repository.
