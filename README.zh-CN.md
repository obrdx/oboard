<div align="center">

# OBoard

面向个人使用的多节点代理控制面板。

[English](README.md) · **简体中文**

<p>
  <a href="https://github.com/obrdx/oboard/releases"><img src="https://img.shields.io/github/v/release/obrdx/oboard?include_prereleases&style=flat-square&logo=github&label=Release&color=2563eb" alt="Release"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/obrdx/oboard?style=flat-square&logo=go&logoColor=white&color=00ADD8" alt="Go">
  <img src="https://img.shields.io/badge/Platform-Linux%20%28amd64%20%7C%20arm64%29-1e293b?style=flat-square&logo=linux&logoColor=white" alt="Platform">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/obrdx/oboard?style=flat-square&logo=gnu&logoColor=white&label=License&color=16a34a" alt="License"></a>
</p>

<p>
  <a href="https://obrdx.github.io/"><img src="https://img.shields.io/badge/%E5%AE%89%E8%A3%85%E4%B8%8E%E6%96%87%E6%A1%A3-obrdx.github.io-0ea5e9?style=flat-square&logo=readthedocs&logoColor=white" alt="安装与文档"></a>
  <a href="https://github.com/obrdx/oboard-agent"><img src="https://img.shields.io/badge/Agent-obrdx%2Foboard--agent-6366f1?style=flat-square&logo=github&logoColor=white" alt="oboard-agent"></a>
</p>

</div>

OBoard 是面向个人及小范围分发的 Linux 多节点代理控制面板，用于集中管理服务器、代理链路、用户订阅、TLS 证书与域名解析，并与节点侧的 [oboard-agent](https://github.com/obrdx/oboard-agent) 配合完成配置下发与状态维护。该项目定位于自托管使用，不适用于商业运营场景。

## 核心特性

- **轻量单目录部署**：基于原生 Go 二进制与内置 SQLite 运行，默认部署于 `/opt/oboard`，无需外部数据库依赖。
- **拓扑化链路编排**：提供可视化多跳代理路径配置，支持端口转发、链式转发、透明中转、WARP、VPNGate 及规则分流。
- **签名下发与热更新**：配置文件与发行包均经过签名校验；用户权限与流量配额变更支持运行时热生效，无需重启代理内核。
- **多端统一管理**：同时提供 Web 面板、REST API 与原生 MCP 接口，支持 Passkey 与 TOTP 二步验证。
- **监控、探测与审计**：内置节点状态监控、延迟与路由探测、DDNS、流量统计及按需启用的连接审计功能。

## 支持范围

| 分类 | 支持项 |
| :--- | :--- |
| **入站协议** | VLESS (TLS / Reality)、Hysteria2、AnyTLS、Shadowsocks、Snell (v4 / v5 / v6)、Mieru、SOCKS5、SSH |
| **转发与出口** | 端口转发、透明中转、WireGuard / SSH 隧道、WARP、VPNGate、外部节点导入 |
| **订阅格式** | Mihomo (Clash Meta)、sing-box、Surge、Stash、Shadowrocket、Loon、Egern、Quantumult X、Surfboard、V2Ray / URI |
| **DNS 与证书** | ACME 自动签发与续期 · Cloudflare、阿里云 DNS、DNSPod、腾讯云 ESA、华为云 DNS |
| **运行平台** | Linux (`amd64` / `arm64`) · `systemd` / `OpenRC` |

## 安装

OBoard 以独立二进制方式运行。安装、升级与卸载步骤请参阅官方文档：

- [安装指南](https://obrdx.github.io/)

默认安装根目录为 `/opt/oboard`，默认监听端口为 `2787`。

## 相关仓库

- [obrdx/oboard-agent](https://github.com/obrdx/oboard-agent)：节点侧 Agent 与内置精简代理内核 `oboard-sb`

## 许可证

Copyright 2026 OBoard contributors.

`oboard` 遵循 [GNU GPL v3](LICENSE) 许可证发布。
