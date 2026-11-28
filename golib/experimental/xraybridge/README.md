# Experimental net-merge VPN Android alpha

The separate ARM64 debug APK is built using `-PexperimentalVpn=true`. It has package ID `org.netmerge.android.vpn.experimental`, uses only the VPN native library and never loads the direct-mode backend. The original build remains available. See the CI artifact `experimental-vpn-apk`.

- Xray v26.11.9 pinned by commit; [upstream source and MPL-2.0 license](https://github.com/XTLS/Xray-core/tree/676eaf66f8f05d4d558ee7a74b4736623d3feeb9).
- Import a supported VLESS/REALITY TCP share link and explicitly supply the trusted endpoint IP. Profiles are encrypted locally using Android Keystore.
- Select app exclusions only if intentional; they bypass the VPN. The VPN application's own UID is excluded for transport sockets.
- Both IPv4 and IPv6 routes are requested, or startup fails. DNS is carried through TUN; live DNS/IPv6 leakage tests are pending.
- A started TUN or successful endpoint TCP probe is not proof of remote authentication. UDP packets above 2048 bytes are rejected. UDP write deadlines are not implemented.
- No automatic fallback to direct mode. If Android tears the VPN down, ordinary traffic may resume unless the system's always-on/block-without-VPN setting is configured. This is not a verified kill switch.
- Only synthetic tests have run so far; do not use for sensitive traffic until real VPN, network switching and leak tests pass. No SOTA subscription import yet.
- Do not send credentials to CI, GitHub issues, logs, or the repository. Debug APK signing may differ between CI runs; uninstalling clears saved settings.

Before distributing a release, audit and package the dependency notices and license obligations.
