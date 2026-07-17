import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
test('VenueFlow dashboard has dispatch data and navigation', async () => {
  const source = await readFile(new URL('../src/App.vue', import.meta.url), 'utf8')
  assert.match(source, /活动订单调度/)
  assert.match(source, /实时线路/)
  assert.match(source, /场地事件中心/)
  assert.match(source, /VN-260716-018/)
})

test('VenueFlow writes use API and idempotency keys', async () => {
  const api = await readFile(new URL('../src/api.js', import.meta.url), 'utf8')
  const source = await readFile(new URL('../src/App.vue', import.meta.url), 'utf8')
  assert.match(api, /Idempotency-Key/)
  assert.match(source, /fleetApi\.assignShipment/)
  assert.match(source, /fleetApi\.resolveException/)
  assert.match(source, /fleetApi\.confirmSettlement/)
})

test('VenueFlow exposes session ticketing and settlement operations', async () => {
  const api = await readFile(new URL('../src/api.js', import.meta.url), 'utf8')
  const source = await readFile(new URL('../src/App.vue', import.meta.url), 'utf8')
  assert.match(api, /sessions/)
  assert.match(api, /checkin/)
  assert.match(api, /settle/)
  assert.match(source, /场次票务日结/)
  assert.match(source, /票码唯一校验/)
  assert.match(source, /完成日结/)
  assert.match(source, /pending-settlement/)
  assert.match(source, /进入结算/)
})
