import { describe, expect, it } from 'vitest'
import { externalProtocolLabel, newThirdPartyNodeDraft, thirdPartyFormIssue, thirdPartyFormPayload } from './ThirdPartyNodes'

describe('third-party node form', () => {
  it('maps HTTPS to an http outbound with TLS and never exposes it to subscriptions', () => {
    const draft = { ...newThirdPartyNodeDraft(3), protocol: 'https' as const, address: 'proxy.example.com', port: 8443, username: 'alice', password: 'pw', server_name: 'edge.example.com', insecure: true, expose_to_users: true }
    const payload = thirdPartyFormPayload(draft)
    expect(payload.protocol).toBe('http')
    expect(payload.expose_to_users).toBe(false)
    expect(JSON.parse(payload.config_json)).toEqual({ username: 'alice', password: 'pw', tls: { enabled: true, server_name: 'edge.example.com', insecure: true } })
    expect(externalProtocolLabel({ protocol: 'http', config_json: payload.config_json })).toBe('HTTPS')
  })

  it('builds socks5 and shadowsocks configs and validates required fields', () => {
    const socks = thirdPartyFormPayload({ ...newThirdPartyNodeDraft(0), address: '203.0.113.5', port: 1080 })
    expect(socks.protocol).toBe('socks')
    expect(JSON.parse(socks.config_json)).toEqual({ version: '5' })
    const ss = { ...newThirdPartyNodeDraft(0), protocol: 'shadowsocks' as const, address: 'ss.example.com', port: 8388 }
    expect(thirdPartyFormIssue(ss)).toContain('密码')
    expect(JSON.parse(thirdPartyFormPayload({ ...ss, password: 'secret' }).config_json)).toEqual({ method: '2022-blake3-aes-128-gcm', password: 'secret' })
    expect(thirdPartyFormIssue({ ...newThirdPartyNodeDraft(0), address: 'x', port: 70000 })).toContain('端口')
  })
})
