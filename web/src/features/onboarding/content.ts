import type { OnboardingItem, OnboardingItemKey, OnboardingView } from './types'

export type OnboardingTarget =
  | { tab: string }
  | { tab: 'settings'; category: string; section: string }

export type OnboardingItemContent = {
  title: string
  summary: string
  description: string
  target?: OnboardingTarget
  targetLabel?: string
  confirmLabel?: string
}

// Copy for every checklist item. Descriptions state what the step is for and
// what completes it, in the same terms the rest of the panel uses.
export const onboardingContent: Record<OnboardingItemKey, OnboardingItemContent> = {
  password: {
    title: '修改初始密码',
    summary: '替换安装时生成的一次性密码。',
    description: '首个管理员账号的密码在安装时随机生成，并以明文形式出现在安装日志中。请立即将其替换为仅由您掌握的密码。修改后当前会话将失效，需要使用新密码重新登录，初始化进度会自动保留。',
  },
  controller_url: {
    title: '设置面板公开地址',
    summary: 'Agent 安装、注册与更新均通过该地址连接面板。',
    description: '填写服务器能够访问到的面板地址。正式环境必须使用 HTTPS；HTTP 仅允许用于本机地址或开发版本。如面板位于反向代理之后，请同时在「设置 → 面板 → 访问」中配置受信任的代理地址段。',
    target: { tab: 'settings', category: 'panel', section: 'access' },
    targetLabel: '打开访问设置',
  },
  node_defaults: {
    title: '确认节点默认参数',
    summary: '拥塞控制、TCP 调优与时间校正的默认值。',
    description: '以下参数仅在服务器首次安装 Agent 时写入，已安装的服务器需重新执行安装命令才会生效。请在接入服务器之前确认；单台服务器可在其设置中另行覆盖。',
    target: { tab: 'settings', category: 'nodes', section: 'general' },
    targetLabel: '打开节点设置',
    confirmLabel: '确认使用当前值',
  },
  server: {
    title: '接入第一台服务器',
    summary: '添加服务器并在目标主机上安装 Agent。',
    description: '在「服务器管理」中添加服务器后，复制生成的安装命令，以 root 身份在目标主机上执行。Agent 连接面板后，服务器状态变为「在线」，此项即自动完成。',
    target: { tab: 'servers' },
    targetLabel: '前往服务器管理',
  },
  entry: {
    title: '创建入口与代理链路',
    summary: '在服务器上创建供客户端连接的入口。',
    description: '在「代理拓扑」中选择入口服务器，于画布空白处右键选择「入口节点」创建入口。如需中转或更换出口，可在入口所在服务器之后继续串联服务器、WARP 或其他出口。存在已启用的入口时，此项自动完成。',
    target: { tab: 'proxy-paths' },
    targetLabel: '前往代理拓扑',
  },
  access: {
    title: '配置套餐并分配用户',
    summary: '套餐决定用户可以使用哪些节点。',
    description: '在「套餐管理」中创建套餐并加入入口节点，然后在「用户与分组」中为用户分配该套餐。如需自行验证，可将套餐分配给当前管理员账号。存在包含节点的套餐且至少一个用户已分配时，此项自动完成。',
    target: { tab: 'plans' },
    targetLabel: '前往套餐管理',
  },
  subscription: {
    title: '导入订阅并验证连通',
    summary: '在客户端中导入订阅，确认可以正常连接。',
    description: '在「节点」页面选择客户端格式并复制订阅链接，导入客户端后确认能够正常访问网络。此项无法由面板自动判断，验证完成后请手动确认。',
    target: { tab: 'nodes' },
    targetLabel: '前往节点',
    confirmLabel: '已完成验证',
  },
  two_factor: {
    title: '启用两步验证',
    summary: '为管理员账号绑定 TOTP 或通行密钥。',
    description: '管理员账号可以修改节点配置，具有等同于服务器 root 的权限。建议绑定认证器应用或通行密钥，防止密码泄露后被直接登录。',
    target: { tab: 'account' },
    targetLabel: '前往我的账户',
  },
  backup: {
    title: '启用定时备份',
    summary: '设置恢复密码并开启定时备份。',
    description: '备份包含数据库快照与证书续期状态，并使用恢复密码加密。请同时妥善保管服务器上的 OBOARD_SESSION_SECRET：该密钥丢失后，已加密的 DNS 凭据与证书私钥将无法读取。',
    target: { tab: 'settings', category: 'panel', section: 'maintenance' },
    targetLabel: '打开更新与维护',
  },
  notifications: {
    title: '配置通知渠道',
    summary: '接收服务器离线、恢复与到期提醒。',
    description: '可添加 Telegram 或 Bark 渠道，并按事件类型选择需要接收的提醒。',
    target: { tab: 'notifications' },
    targetLabel: '前往通知中心',
  },
  dns_credentials: {
    title: '添加 DNS 服务商凭据',
    summary: '用于域名解析同步与 DNS-01 证书签发。',
    description: '使用 AnyTLS、Hysteria2 等需要证书的协议时，面板可通过 DNS-01 自动签发证书。凭据加密保存在面板中，不会下发到服务器。',
    target: { tab: 'dns-records' },
    targetLabel: '前往域名解析',
  },
  updates: {
    title: '检查更新策略',
    summary: '面板与 Agent 的自动更新、维护时间窗口。',
    description: '确认是否启用面板与 Agent 自动更新、更新时间窗口及并发数量。中国大陆的服务器可在此选择资源下载源。',
    target: { tab: 'settings', category: 'panel', section: 'maintenance' },
    targetLabel: '打开更新与维护',
    confirmLabel: '已检查',
  },
  audit: {
    title: '检查审计策略',
    summary: '订阅审计与连接审计默认关闭。',
    description: '审计用于识别订阅共享与异常设备。启用前请确认处理方式：「警告」仅提示，「限制」会在证据充分时暂停对应设备。',
    target: { tab: 'audit' },
    targetLabel: '前往审计中心',
    confirmLabel: '已检查',
  },
  security: {
    title: '检查注册与远程访问',
    summary: '用户注册、远程终端等高权限入口。',
    description: '确认是否开放用户注册及新用户的默认分组，并确认远程终端等远程访问能力是否需要启用。不使用的能力建议保持关闭。',
    target: { tab: 'settings', category: 'panel', section: 'security' },
    targetLabel: '打开安全设置',
    confirmLabel: '已检查',
  },
  retention: {
    title: '检查数据保留期限',
    summary: '监控、延迟与审计数据的保留天数。',
    description: '保留期限越长，数据库占用越大。请根据磁盘容量与排查需求调整。',
    target: { tab: 'settings', category: 'database', section: 'retention' },
    targetLabel: '打开保留策略',
    confirmLabel: '已检查',
  },
}

export const onboardingTours = [
  {
    key: 'proxy_path',
    title: '代理链路',
    summary: '入口、中转、出口与分流规则的配置方法。',
    duration: '约 2 分钟',
  },
] as const

export function itemsByLevel(view: OnboardingView | null, level: OnboardingItem['level']) {
  return (view?.items || []).filter(item => item.level === level)
}

export function firstOpenRequired(view: OnboardingView | null): OnboardingItemKey | null {
  return itemsByLevel(view, 'required').find(item => item.status === 'pending')?.key || null
}

// guideTracked decides whether the dashboard card and resume prompt appear.
// Established panels without guide state stay quiet until opened by hand.
export function guideTracked(view: OnboardingView | null) {
  if (!view) return false
  if (view.mode === 'established') return false
  return view.state.status !== 'completed'
}
