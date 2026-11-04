# 安全模型

## 执行链路

```text
计划 / 事件 / 手动 ─▶ PluginRun（持久化）─▶ oboard-plugin-worker ─▶ 一次性 Runner ─▶ SDK 调用
                                                   │ Unix Socket（Controller 所有）     │
                                                   ▼                                    ▼
                                              Capability Gateway ─▶ 结构化操作 ─▶ （诊断）签名 Agent 任务
```

| 组件 | 职责 | 不得承担 |
|---|---|---|
| Controller | 保存插件、实例、授权、加密密钥、状态、计划与执行记录；Capability Gateway | 不在自身进程执行插件代码 |
| Plugin Worker | 领取执行、创建并监管 Runner、转发 SDK 调用 | 不持有数据库、会话、Agent Token；无 IP 网络 |
| Runner | 执行一次 `main.js` | 不能访问网络、文件、子进程或系统接口 |
| Agent | 执行结构化诊断任务 | 不执行插件代码、不接受命令 |

## Runner 隔离

- bubblewrap `--unshare-all`（无网络命名空间出口）、只读最小挂载，仅继承 RPC 描述符。
- 委派的 cgroup v2：内存（默认 64 MiB，上限 128 MiB）、进程数、CPU；OOM 通过 `memory.events` 识别。
- `no_new_privs`、rlimits（文件描述符、core、文件大小、进程数）。
- seccomp：拒绝 `execve`/`execveat`、非线程的 `clone`、`clone3`、`socket`/`connect`/`bind`、`ptrace`、`mount`、`unshare`/`setns`、`bpf`、`keyctl`、模块加载、`process_vm_*`、`memfd_create` 等；外来架构直接终止。
- 运行时不注入 `require`、`process`、文件、套接字、定时器；源码中的 `require()` / `import()` 在安装和发布时即被拒绝，源码映射注释不会被读取。
- Worker 的 systemd 单元 `IPAddressDeny=any`、`RestrictAddressFamilies=AF_UNIX AF_NETLINK`，以非特权用户运行。隔离条件不满足时只禁用插件执行。

## 能力目录

| 能力 | 资源范围 | 风险 | 审计 |
|---|---|---|---|
| `servers.read` / `servers.health.read` / `servers.metrics.read` | 服务器 | 低 | |
| `network.ping` / `network.trace` / `network.tcp_probe` / `network.http_probe` | 服务器 | 中 | ✓ |
| `network.dns_lookup` | 服务器 | 低 | ✓ |
| `http.request` | HTTP 主机 | 高 | ✓ |
| `lan.request` | 服务器 + 内网目标 | 高 | ✓ |
| `state.read` / `state.write` | 本实例 | 低 | |
| `secrets.use` | 本实例密钥 | 高 | ✓ |
| `notifications.send` | 通知渠道 | 中 | ✓ |
| `users.read` | 用户 | 低 | |
| `users.notify` | 用户 | 中 | ✓ |
| `plans.read` | 套餐 | 低 | |
| `plans.notify` | 套餐 | 中 | ✓ |
| `events.server_status` | 服务器 | 低 | |
| `ui.page` | 无 | 低 | |

每项能力有固定参数 Schema、超时、每分钟调用上限和每次执行的配额（SDK 调用、HTTP 请求、Agent 操作）。

## 有效权限

```
有效权限 = 清单声明 ∩ 管理员授权 ∩ 资源范围 ∩ 运行策略
```

每次 SDK 调用都重新检查：执行租约仍有效、全局与插件开关仍开启、能力在当前版本清单中、授权包含该能力且资源在范围内、发起者（手动执行时的用户或服务账号）仍有权访问该资源、配额与速率未超。授权按实例保存，只能是清单声明的子集；环境变量中的服务器选择从不构成授权。新版本若扩大权限（新增能力、主机、方法、事件、密钥或资源上限），所有实例进入“待审核权限”，管理员重新保存授权前不会运行。

发布草稿（`plugins.publish`）、授权与撤销（`plugin_instances.grant` / `plugin_instances.grant.revoke`）、启停实例（`plugin_instances.enable` / `disable`）与草稿试运行（`plugins.drafts.test_run`）是管理员操作：管理员账号在面板上能做，该账号授权的 MCP 也能做，调用即生效，没有单独的审批。两道保险始终有效：授权只能是清单声明过的能力、主机与内网目标，多出来的直接报错；每次变更写入实例审计（`manage.*`），记录操作者、OAuth 授权、新增与移除的权限项以及上一版授权，可用上一版授权再次调用撤回，或一键撤销全部授权。服务账号不能执行这些操作。安装与更新插件包、切换版本、卸载、密钥与运行策略仍只允许交互式管理员在面板操作。

## Agent 诊断

诊断是签名任务 `probe_network_ping|trace|tcp|dns|http`（协议版本 1、来源 `plugin`），不含命令、程序路径或参数列表。Agent：

- 以原生 Go 套接字实现，不调用 `ping`、`traceroute`、`curl`；
- 严格解码（拒绝未知字段），校验过期时间；
- 解析目标后要求**所有**地址都是公网地址（混合应答整体拒绝），HTTP 探测的每次重定向重新校验；
- 受节点本地 `plugins_enabled` 开关约束（Standard 默认允许、Hardened 默认拒绝，节点可显式 allow/deny）。Standard 节点可由主控经 `servers.agent_policy.set` 远程开关；Hardened 节点主控只能关闭，打开需在节点执行 `obag remote-access allow plugins`；
- 每台服务器同时只有一个诊断，主控等待有界时间后放弃并撤回排队任务。

## 内网请求

`lan.request` 是签名任务 `plugin_lan_request`（来源只能是 `plugin`），与诊断共用每台服务器一个在途任务的限制：

- 目标只来自清单 `lan.targets`：精确的私有 IP 字面量（`10/8`、`172.16/12`、`192.168/16`、`100.64/10`、`fc00::/7`）、端口、协议与路径前缀；不接受网段、域名、回环、链路本地或公网地址；
- 有效权限 = 清单目标 ∩ 授权勾选的目标 ∩ 授权服务器 ∩ 节点本地 `lan` 开关。`lan` 开关在 Standard 与 Hardened 模式下都默认关闭，且同时要求插件开关打开。可在节点执行 `obag remote-access allow lan` 打开；Standard 节点也可由主控经 `servers.agent_policy.set` 打开（同时打开插件开关），Hardened 节点主控只能关闭；
- 目标的地址、端口、协议、前缀、方法、`side_effect` 或 `insecure_tls` 变化都算权限扩大，需要重新审核；
- Agent 独立复核：拒绝非内网地址与本机网卡地址，只拨号目标字面地址，不使用代理、不跟随重定向，响应体最多 16 KiB（默认 4 KiB），丢弃 `Set-Cookie`；
- `side_effect` 目标的每次调用都写入审计（目标端点、方法、路径、状态码），面板手动运行前需要确认。

## 草稿试运行

交互式管理员可以在编辑器中把草稿在一个已发布、已授权的实例上试运行一次。草稿代码使用该实例已保存的配置，权限取已发布版本授权与草稿声明的交集：草稿新增或修改的能力、主机、方法与内网目标在发布并重新审核前都会被拒绝。试运行中，标记 `side_effect` 的内网目标与 `notifications.send`、`users.notify`、`plans.notify` 一律拒绝（`DRAFT_SIDE_EFFECT_BLOCKED`），只读目标照常可用。试运行记录标记为草稿，不计入实例的连续失败暂停；管理员账号及其授权的 MCP 可发起，服务账号不能。

## 密钥与审计

密钥以 `OBOARD_SESSION_SECRET` 加密存储，写入后不可读取；代码只拿到 `SecretRef`。所有敏感能力调用写入插件活动审计（能力、资源、结果、错误码、耗时；失败时附拒绝原因），不含请求/响应内容或密钥。执行日志与结果在保存前遮盖密钥。
