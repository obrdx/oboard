<div align="center">

# OBoard

A self-hosted Linux control panel for personal multi-node proxy networks.

**English** · [简体中文](README.zh-CN.md)

<p>
  <a href="https://github.com/obrdx/oboard/releases"><img src="https://img.shields.io/github/v/release/obrdx/oboard?include_prereleases&style=flat-square&logo=github&label=Release&color=2563eb" alt="Release"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/obrdx/oboard?style=flat-square&logo=go&logoColor=white&color=00ADD8" alt="Go">
  <img src="https://img.shields.io/badge/Platform-Linux%20%28amd64%20%7C%20arm64%29-1e293b?style=flat-square&logo=linux&logoColor=white" alt="Platform">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/obrdx/oboard?style=flat-square&logo=gnu&logoColor=white&label=License&color=16a34a" alt="License"></a>
</p>

<p>
  <a href="https://obrdx.github.io/"><img src="https://img.shields.io/badge/Install%20%26%20Docs-obrdx.github.io-0ea5e9?style=flat-square&logo=readthedocs&logoColor=white" alt="Install and Docs"></a>
  <a href="https://github.com/obrdx/oboard-agent"><img src="https://img.shields.io/badge/Agent-obrdx%2Foboard--agent-6366f1?style=flat-square&logo=github&logoColor=white" alt="oboard-agent"></a>
</p>

</div>

OBoard is a self-hosted Linux control panel for personal and small-group multi-node proxy networks. It centrally manages servers, proxy paths, subscriptions, TLS certificates, and DNS records, working with [oboard-agent](https://github.com/obrdx/oboard-agent) for signed configuration delivery and node maintenance. It is designed for private deployment rather than commercial operation.

## Features

- **Single-root deployment**: Runs as a native Go binary with embedded SQLite under `/opt/oboard`, requiring no external database.
- **Topology-based routing**: Visual multi-hop proxy path configuration supporting port forwarding, chained proxies, transparent relays, WARP, VPNGate, and rule-based routing.
- **Signed delivery and hot updates**: Signed configurations and release verification, with runtime user and quota updates applied without restarting the proxy kernel.
- **Unified management interfaces**: Provides a Web panel, REST API, and native MCP endpoint, secured with Passkeys and TOTP 2FA.
- **Monitoring, probing, and auditing**: Built-in node telemetry, latency and route probes, DDNS, traffic accounting, and optional connection auditing.

## Compatibility

| Category | Supported |
| :--- | :--- |
| **Inbounds** | VLESS (TLS / Reality), Hysteria2, AnyTLS, Shadowsocks, Snell (v4 / v5 / v6), Mieru, SOCKS5, SSH |
| **Forwarding & Exits** | Port forwarding, transparent relays, WireGuard / SSH tunnels, WARP, VPNGate, external node imports |
| **Subscriptions** | Mihomo (Clash Meta), sing-box, Surge, Stash, Shadowrocket, Loon, Egern, Quantumult X, Surfboard, V2Ray / URI |
| **DNS & Certificates** | Automated ACME issuance and renewal · Cloudflare, AliDNS, DNSPod, Tencent ESA, Huawei Cloud DNS |
| **Platforms** | Linux (`amd64` / `arm64`) · `systemd` / `OpenRC` |

## Installation

OBoard runs as a standalone binary. Refer to the official documentation for installation, upgrades, and uninstallation:

- [Installation Guide](https://obrdx.github.io/)

Default installation root: `/opt/oboard` · Default listen port: `2787`

## Related Repositories

- [obrdx/oboard-agent](https://github.com/obrdx/oboard-agent) — Node-side Agent and the slim `oboard-sb` proxy kernel

## License

Copyright 2026 OBoard contributors.

`oboard` is released under the [GNU GPL v3](LICENSE).
