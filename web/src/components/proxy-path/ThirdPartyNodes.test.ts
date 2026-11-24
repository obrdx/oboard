import { describe, expect, it } from 'vitest'
import { externalProtocolChainOnly, externalProtocolLabel, newThirdPartyNodeDraft, thirdPartyFormIssue, thirdPartyFormPayload } from './ThirdPartyNodes'

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

  it('builds a chain-only WireGuard peer config and validates keys', () => {
    const key = 'YNXtAzepDqRv9H52osJVDQnznT5AL11IUmHwhGvYPXE='
    const draft = { ...newThirdPartyNodeDraft(0), protocol: 'wireguard' as const, address: 'wg.example.com', port: 51820, expose_to_users: true }
    expect(thirdPartyFormIssue(draft)).toContain('私钥')
    const filled = { ...draft, private_key: key, public_key: key, wg_address: '172.16.0.2/32, fd00::2/128', reserved: '1,2,3', mtu: 1280 }
    expect(thirdPartyFormIssue(filled)).toBe('')
    expect(thirdPartyFormIssue({ ...filled, reserved: '1,2' })).toContain('Reserved')
    const payload = thirdPartyFormPayload(filled)
    expect(payload.protocol).toBe('wireguard')
    expect(payload.expose_to_users).toBe(false)
    expect(payload.name).toBe('WireGuard wg.example.com')
    expect(JSON.parse(payload.config_json)).toEqual({ private_key: key, address: ['172.16.0.2/32', 'fd00::2/128'], peer_public_key: key, reserved: [1, 2, 3], mtu: 1280 })
    expect(externalProtocolChainOnly('wireguard')).toBe(true)
    expect(externalProtocolLabel({ protocol: 'wireguard', config_json: '{}' })).toBe('WireGuard')
  })
})
