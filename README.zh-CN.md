<div align="center">

# OBoard

**面向个人多节点代理网络的自托管 Linux 控制面板。**

[English](README.md) · **简体中文**

<p>
  <a href="https://github.com/obrdx/oboard/releases"><img src="https://img.shields.io/github/v/release/obrdx/oboard?include_prereleases&style=flat-square&logo=github&label=Release&color=2563eb" alt="Release"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/obrdx/oboard?style=flat-square&logo=go&logoColor=white&color=00ADD8" alt="Go Version">
  <img src="https://img.shields.io/badge/Platform-Linux%20%28amd64%20%7C%20arm64%29-1e293b?style=flat-square&logo=linux&logoColor=white" alt="Platform">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/obrdx/oboard?style=flat-square&logo=gnu&logoColor=white&label=License&color=16a34a" alt="License"></a>
</p>

<p>
  <a href="https://obrdx.github.io/"><img src="https://img.shields.io/badge/%E6%96%87%E6%A1%A3%E4%B8%8E%E5%AE%89%E8%A3%85-obrdx.github.io-0ea5e9?style=flat-square&logo=readthedocs&logoColor=white" alt="文档与安装"></a>
  <a href="https://github.com/obrdx/oboard-agent"><img src="https://img.shields.io/badge/%E8%8A%82%E7%82%B9%20Agent-obrdx%2Foboard--agent-6366f1?style=flat-square&logo=github&logoColor=white" alt="oboard-agent"></a>
</p>

</div>

---

## ✨ 项目简介

**OBoard** 是一个轻量级的自托管 Linux 代理控制平面，基于单个 Go 主程序与 SQLite 集中管理服务器、多跳代理链路、用户订阅、DNS 解析与 TLS 证书，并向运行精简内核 `oboard-sb` 的节点侧 [oboard-agent](https://github.com/obrdx/oboard-agent) 下发已签名的配置与发行包。

> 定位为**个人与小范围自用**的多节点代理运维面板，专注于稳定部署、安全边界与清晰运维，而非商业计费系统。

---

## ⚡ 核心优势

- **📦 单目录极简部署**：主控程序、Web 界面、离线 GeoIP 与已签名 Agent / 内核资源统一收纳在单个根目录（默认 `/opt/oboard`）下，内置 SQLite，无需额外数据库，整目录迁移即刻复用。
- **🕸️ 拓扑化代理链路**：可视化编排多跳代理链路，支持 Realm / nftables 端口转发、可信透明转发、WireGuard 与独立 SSH 隧道、WARP / VPNGate 出口及 IPv4 / IPv6 分流路由。
- **🔐 签名校验与热更新**：支持一次性令牌接入、Ed25519 发行包验签、签名任务下发与短期授权租约；支持运行时热下发用户凭证，变更权限无需重启代理内核。
- **🤖 Web · REST · MCP 一体化**：Web 控制台、`/api/v1` REST 接口与原生远程 **MCP**（`/api/v1/mcp`）共享同一权限与能力层，支持 Passkey / TOTP 二步验证、服务账号与变更集审计。
- **📡 探测、路由与审计**：内置入站、出口、MTU、延迟探测与 NextTrace 路由监测，支持探针 DDNS、流量配额控制以及按需开启的连接审计台。
- **🧩 模块化扩展**：按需启用无状态边缘订阅中继（`oboard-subscription-relay`）、独立隔离的 AI 诊断进程（`oboard-ai-worker`）与沙箱插件运行环境。

---

## 🔌 协议与生态支持

| 类别 | 支持范围 |
| :--- | :--- |
| **托管入站协议** | `VLESS`（TLS / Reality）、`Hysteria2`（TLS / Salamander）、`AnyTLS`、`Shadowsocks`（AEAD / 2022 / UoT）、`Snell`（v4 / v5 / v6）、`Mieru`（TCP / UDP 多端口）、`SOCKS5`、独立受限 `SSH`（支持 `udpgw`） |
| **出口与组网** | 直连、多服务器链式代理、Cloudflare `WARP`、`VPNGate` 出口池、外部 `WireGuard`、`HTTP / HTTPS CONNECT`、第三方导入节点 |
| **订阅格式** | `Mihomo (Clash Meta)`、`sing-box`、`Surge` / `Surge Mac`、`Stash`、`Shadowrocket`、`Loon`、`Egern`、`Quantumult X`、`Surfboard`、`V2Ray / URI`（支持客户端自动识别） |
| **DNS 与证书** | ACME 自动签发与续期（主控 DNS-01 / 节点 HTTP-01）· `Cloudflare`、`阿里云 DNS`、`DNSPod`、`腾讯云 ESA`、`华为云 DNS` |
| **运行平台** | Linux `amd64` / `arm64` · `systemd` 与 `OpenRC`（Debian/Ubuntu、Alpine、RHEL 系、openSUSE、Arch） |

---

## 🏗️ 架构概览

```text
                    ┌──────────────────────────────────────────┐
  Web 面板 / API ──▶│             OBoard 主控面板              │◀── 远程 MCP (/api/v1/mcp)
      客户端订阅 ◀──│    Go 主程序 · SQLite · 已签名发行资产   │──▶ 订阅中继 Relay（可选）
                    └────────────────────┬─────────────────────┘
                                         │ 签名任务 · 授权租约 · 运行时用户热更新 (WSS)
                    ┌────────────────────┴─────────────────────┐
                    ▼                                          ▼
         ┌──────────────────────┐                  ┌──────────────────────┐
         │     oboard-agent     │                  │     oboard-agent     │
         │ oboard-sb · 隧道转发 │ ◀── 多跳链路 ──▶ │ oboard-sb · 探针诊断 │
         └──────────────────────┘                  └──────────────────────┘
```

---

## 🚀 安装方式

OBoard 主控以原生二进制方式运行在 Linux 服务器上，安装、更新与卸载请参考官方指引：

👉 **[安装指南与文档 — obrdx.github.io](https://obrdx.github.io/)**

- **默认安装根目录**：`/opt/oboard`
- **默认访问端口**：`2787`
- **节点接入**：在 Web 面板中添加服务器即可生成一次性节点安装命令（支持在首次安装时自动配置 Linux BBR、TCP 调优与宿主机时间同步）。

---

## 🔗 相关仓库

- **[obrdx/oboard-agent](https://github.com/obrdx/oboard-agent)** — 节点侧 Agent 守护进程、本地运维工具 `obag` 与精简代理内核 `oboard-sb`。

---

## 📄 许可证

Copyright 2026 OBoard contributors.

`oboard` 基于 [GNU General Public License v3.0](LICENSE) 许可证发布。
