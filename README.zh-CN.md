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

OBoard 是一个轻量级的 Linux 多节点代理控制面板，用来集中管理自己的服务器、代理链路、订阅、证书和域名解析。搭配节点端的 [oboard-agent](https://github.com/obrdx/oboard-agent) 使用，面向个人和小范围自用，不做商业售卖面板。

## 为什么用 OBoard

- **部署省事**：二进制直接运行，内置 SQLite，所有文件都在 `/opt/oboard` 下，不用配外部数据库。
- **链路清晰**：可视化配置多跳代理，支持端口转发、中转链式代理、WARP、VPNGate 和规则分流。
- **变更无感**：配置和安装包带签名校验，修改用户权限和流量配额支持热生效，不用重启代理内核。
- **管理顺手**：提供 Web 面板、REST API 和原生 MCP 接口，支持 Passkey / 二步验证。
- **状态可见**：自带节点监控、延迟和路由探针、DDNS、流量统计以及按需开启的审计功能。

## 支持内容

| 分类 | 支持项 |
| :--- | :--- |
| **入站协议** | VLESS (TLS / Reality)、Hysteria2、AnyTLS、Shadowsocks、Snell (v4 / v5 / v6)、Mieru、SOCKS5、SSH |
| **转发与出口** | 端口转发、透明中转、WireGuard / SSH 隧道、WARP、VPNGate、第三方节点导入 |
| **订阅格式** | Mihomo (Clash Meta)、sing-box、Surge、Stash、Shadowrocket、Loon、Egern、Quantumult X、Surfboard、V2Ray / URI |
| **DNS 与证书** | ACME 自动签发续期 · Cloudflare、阿里云 DNS、DNSPod、腾讯云 ESA、华为云 DNS |
| **系统平台** | Linux (`amd64` / `arm64`) · `systemd` / `OpenRC` |

## 安装

OBoard 以二进制方式运行，完整安装、更新与卸载方式见官方页面：

- [安装指南](https://obrdx.github.io/)

默认安装目录为 `/opt/oboard`，默认端口为 `2787`。

## 相关仓库

- [obrdx/oboard-agent](https://github.com/obrdx/oboard-agent)：节点端 Agent 与精简代理内核 `oboard-sb`

## 许可证

Copyright 2026 OBoard contributors.

`oboard` 基于 [GNU GPL v3](LICENSE) 开源。
