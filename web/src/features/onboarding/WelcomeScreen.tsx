import React from 'react'
import { m, useReducedMotion } from 'motion/react'
import { KeyRound, Server, UsersRound } from 'lucide-react'
import { MotionDialogPanel } from '../../components/ui/motion'
import logo from '../../assets/logo.png'

const stages = [
  { icon: KeyRound, title: '基础安全与访问', detail: '修改初始密码，设置面板公开地址，确认节点默认参数。' },
  { icon: Server, title: '接入服务器与入口', detail: '在服务器上安装 Agent，创建供客户端连接的入口。' },
  { icon: UsersRound, title: '分配与验证', detail: '创建套餐并分配用户，在客户端中导入订阅并验证连通。' },
]

export function WelcomeScreen({ onStart, onDemo, onLater }: { onStart: () => void; onDemo: () => void; onLater: () => void }) {
  const reduced = useReducedMotion()
  return <MotionDialogPanel onCancel={onLater} className="onboarding-welcome" ariaLabel="欢迎使用 OBoard">
    <div className="onboarding-welcome-body">
      <header>
        <span className="onboarding-welcome-mark" aria-hidden="true"><img src={logo} alt="" width={40} height={40} /></span>
        <h2>欢迎使用 OBoard</h2>
        <p>首次使用前，需要完成以下基础配置。进度会自动保存，可随时中断后继续。</p>
      </header>
      <ol className="onboarding-stages">
        {stages.map((stage, index) => {
          const Icon = stage.icon
          return <m.li
            key={stage.title}
            initial={reduced ? false : { opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: reduced ? 0 : 0.08 + index * 0.07, duration: 0.32, ease: [0.22, 1, 0.36, 1] }}
          >
            <span className="onboarding-stage-icon"><Icon size={16} /></span>
            <span className="onboarding-stage-copy"><strong>{index + 1}. {stage.title}</strong><small>{stage.detail}</small></span>
          </m.li>
        })}
      </ol>
      <footer>
        <button type="button" onClick={onStart}>开始配置</button>
        <button type="button" className="ghost" onClick={onDemo}>观看代理链路演示</button>
        <button type="button" className="ghost onboarding-welcome-later" onClick={onLater}>稍后配置</button>
      </footer>
      <p className="onboarding-welcome-note">之后可通过顶栏的「初始化与教程」或总览页重新打开。</p>
    </div>
  </MotionDialogPanel>
}
