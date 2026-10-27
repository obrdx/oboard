import { expect, it } from 'vitest'
import { enrolledDaysLabel, hostUptimeLabel } from './fleet-format'

const now = new Date('2026-10-28T12:00:00Z')

it('reports host uptime from the boot time only while online', () => {
  expect(hostUptimeLabel({ status: 'online', host_booted_at: '2026-10-28T11:48:00Z' }, now)).toBe('12 分钟')
  expect(hostUptimeLabel({ status: 'online', host_booted_at: '2026-10-28T06:30:00Z' }, now)).toBe('5 小时 30 分')
  expect(hostUptimeLabel({ status: 'online', host_booted_at: '2026-10-25T09:00:00Z' }, now)).toBe('3 天 3 小时')
  expect(hostUptimeLabel({ status: 'offline', host_booted_at: '2026-10-25T09:00:00Z' }, now)).toBe('')
  expect(hostUptimeLabel({ status: 'online' }, now)).toBe('')
})

it('keeps the enrolled age separate from host uptime', () => {
  expect(enrolledDaysLabel({ created_at: '2026-09-28T12:00:00Z' }, now)).toBe('30 天')
})
