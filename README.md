<div align="center">

# OBoard

**A self-hosted Linux control plane for personal multi-node proxy networks.**

**English** · [简体中文](README.zh-CN.md)

<p>
  <a href="https://github.com/obrdx/oboard/releases"><img src="https://img.shields.io/github/v/release/obrdx/oboard?include_prereleases&style=flat-square&logo=github&label=Release&color=2563eb" alt="Release"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/obrdx/oboard?style=flat-square&logo=go&logoColor=white&color=00ADD8" alt="Go Version">
  <img src="https://img.shields.io/badge/Platform-Linux%20%28amd64%20%7C%20arm64%29-1e293b?style=flat-square&logo=linux&logoColor=white" alt="Platform">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/obrdx/oboard?style=flat-square&logo=gnu&logoColor=white&label=License&color=16a34a" alt="License"></a>
</p>

<p>
  <a href="https://obrdx.github.io/"><img src="https://img.shields.io/badge/Docs%20%26%20Install-obrdx.github.io-0ea5e9?style=flat-square&logo=readthedocs&logoColor=white" alt="Docs and Install"></a>
  <a href="https://github.com/obrdx/oboard-agent"><img src="https://img.shields.io/badge/Node%20Agent-obrdx%2Foboard--agent-6366f1?style=flat-square&logo=github&logoColor=white" alt="oboard-agent"></a>
</p>

</div>

---

## ✨ Overview

**OBoard** centrally manages servers, multi-hop proxy topologies, users, subscriptions, DNS records, and TLS certificates from a single lightweight Go binary backed by SQLite. It distributes signed configurations and verified releases to node-side [oboard-agent](https://github.com/obrdx/oboard-agent) instances running the slim `oboard-sb` kernel.

> Built for **personal and small-group** proxy networks — focused on reliability, security boundaries, and clean operations rather than commercial billing.

---

## ⚡ Why OBoard

- **📦 Single-Root Deployment**: Controller, Web UI, bundled GeoIP, and signed Agent/kernel assets live under one self-contained directory (`/opt/oboard` by default) with SQLite — no external database required.
- **🕸️ Topology-First Networking**: Design multi-hop proxy chains visually with support for Realm/nftables port forwarding, authenticated transparent forwarding, WireGuard & isolated SSH tunnels, WARP & VPNGate exits, and IPv4/IPv6 family-split routing.
- **🔐 Signed Control Plane**: One-time token Agent enrollment, Ed25519-signed release distribution, signed task execution, short-lived authorization leases, and hot runtime user updates without restarting the proxy kernel.
- **🤖 Unified Web · REST · MCP Surface**: One capability and RBAC layer powers the Web panel, `/api/v1` REST API, and native remote **MCP** (`/api/v1/mcp`) with Passkey/WebAuthn, TOTP 2FA, Service Accounts, and audited Changesets.
- **📡 Probing, Routing & Audit**: Built-in inbound/egress/MTU/latency probes, NextTrace route visibility, Probe DDNS, traffic quota enforcement, and an optional connection audit console.
- **🧩 Modular Extensions**: Optional isolated workers for stateless edge subscription relays (`oboard-subscription-relay`), AI diagnostics (`oboard-ai-worker`), and sandboxed plugins.

---

## 🔌 Protocol & Ecosystem Support

| Category | Supported |
| :--- | :--- |
| **Managed Inbounds** | `VLESS` (TLS / Reality), `Hysteria2` (TLS / Salamander), `AnyTLS`, `Shadowsocks` (AEAD / 2022 / UoT), `Snell` (v4 / v5 / v6), `Mieru` (TCP / UDP, multi-port), `SOCKS5`, Restricted `SSH` (`udpgw`) |
| **Exits & Chains** | Direct, Multi-Hop Server Chains, Cloudflare `WARP`, `VPNGate` Pools, External `WireGuard`, `HTTP / HTTPS CONNECT`, Imported Third-Party Nodes |
| **Subscription Formats** | `Mihomo (Clash Meta)`, `sing-box`, `Surge` / `Surge Mac`, `Stash`, `Shadowrocket`, `Loon`, `Egern`, `Quantumult X`, `Surfboard`, `V2Ray / URI` (with auto client detection) |
| **DNS & Certificates** | ACME TLS (Controller DNS-01 & Agent HTTP-01) · `Cloudflare`, `AliDNS`, `DNSPod`, `Tencent ESA`, `Huawei Cloud DNS` |
| **Host Platforms** | Linux `amd64` / `arm64` · `systemd` & `OpenRC` (Debian/Ubuntu, Alpine, RHEL-family, openSUSE, Arch) |

---

## 🏗️ Architecture

```text
                    ┌──────────────────────────────────────────┐
   Web UI / REST ──▶│            OBoard Controller             │◀── Remote MCP (/api/v1/mcp)
   Subscriptions ◀──│   Go Binary · SQLite · Signed Releases   │──▶ Subscription Relay (optional)
                    └────────────────────┬─────────────────────┘
                                         │ Signed Tasks · Auth Leases · Runtime Users (WSS)
                    ┌────────────────────┴─────────────────────┐
                    ▼                                          ▼
         ┌──────────────────────┐                   ┌──────────────────────┐
         │     oboard-agent     │                   │     oboard-agent     │
         │  oboard-sb · Tunnels │   ◀── Chains ──▶  │  oboard-sb · Probes  │
         └──────────────────────┘                   └──────────────────────┘
```

---

## 🚀 Installation

OBoard runs as a native Linux binary. Follow the official guide for interactive installation, updates, and uninstall:

👉 **[Installation & Documentation — obrdx.github.io](https://obrdx.github.io/)**

- **Default install root**: `/opt/oboard`
- **Default web & API port**: `2787`
- **Node onboarding**: Add a server in the Web panel to generate a one-time Agent install command (with optional Linux BBR / TCP tuning and host time-sync setup).

---

## 🔗 Related Repositories

- **[obrdx/oboard-agent](https://github.com/obrdx/oboard-agent)** — Node-side Agent daemon, local `obag` CLI, and the slim `oboard-sb` proxy kernel.

---

## 📄 License

Copyright 2026 OBoard contributors.

`oboard` is released under the [GNU General Public License v3.0](LICENSE).
