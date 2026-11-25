<div align="center">

# OBoard

A lightweight multi-node proxy control panel for personal Linux servers.

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

OBoard is a self-hosted Linux control panel for managing personal multi-node proxy networks. It handles servers, proxy chains, subscriptions, TLS certificates, and DNS in one place, working together with [oboard-agent](https://github.com/obrdx/oboard-agent) on each node. Designed for personal and small-group use, not commercial resale.

## Why OBoard

- **Simple setup**: Runs as a single Go binary with built-in SQLite under `/opt/oboard` — no external database to maintain.
- **Visual routing**: Build multi-hop proxy paths with port forwarding, chained relays, WARP, VPNGate, and rule-based routing.
- **Safe, smooth updates**: Signed configs and releases with hot user/quota updates that do not restart the proxy kernel.
- **Flexible control**: Manage everything from the Web UI, REST API, or native MCP, secured with Passkeys and 2FA.
- **Built-in visibility**: Server health, latency and route probes, DDNS, traffic stats, and optional connection auditing.

## What's Supported

| Category | Supported |
| :--- | :--- |
| **Protocols** | VLESS (TLS / Reality), Hysteria2, AnyTLS, Shadowsocks, Snell (v4 / v5 / v6), Mieru, SOCKS5, SSH |
| **Forwarding & Exits** | Port forwarding, transparent relays, WireGuard / SSH tunnels, WARP, VPNGate, imported third-party nodes |
| **Subscriptions** | Mihomo (Clash Meta), sing-box, Surge, Stash, Shadowrocket, Loon, Egern, Quantumult X, Surfboard, V2Ray / URI |
| **DNS & TLS** | Automatic ACME certificates · Cloudflare, AliDNS, DNSPod, Tencent ESA, Huawei Cloud DNS |
| **Platforms** | Linux (`amd64` / `arm64`) · `systemd` / `OpenRC` |

## Installation

OBoard runs as a native binary. See the official guide for installation, updates, and uninstall:

- [Install Guide](https://obrdx.github.io/)

Default install directory: `/opt/oboard` · Default port: `2787`

## Related

- [obrdx/oboard-agent](https://github.com/obrdx/oboard-agent) — Node-side Agent and the slim `oboard-sb` proxy kernel

## License

Copyright 2026 OBoard contributors.

`oboard` is released under the [GNU GPL v3](LICENSE).
