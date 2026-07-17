const API_BASE = (import.meta.env.VITE_API_BASE || '/api/v1').replace(/\/$/, '')

export async function request(path, options = {}) {
  const response = await fetch(`${API_BASE}${path}`, { headers: { 'Content-Type': 'application/json', ...(options.headers || {}) }, ...options })
  const body = await response.json().catch(() => ({}))
  if (!response.ok || body.code !== 0) throw new Error(body.message || `请求失败（${response.status}）`)
  return body.data
}

function write(path, payload) {
  return request(path, { method: 'POST', headers: { 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(payload || {}) })
}

export const fleetApi = {
  dashboard: () => request('/dashboard'),
  shipments: () => request('/shipments?page=1&pageSize=50'),
  exceptions: () => request('/exceptions?status=待处理'),
  createShipment: (payload) => write('/shipments', payload),
  assignShipment: (id, driver) => write(`/shipments/${encodeURIComponent(id)}/assign`, { driver, actor: '许汝林' }),
  advanceShipment: (id, status) => write(`/shipments/${encodeURIComponent(id)}/status`, { status, actor: '许汝林', note: '工作台操作' }),
  resolveException: (id) => write(`/exceptions/${encodeURIComponent(id)}/resolve`),
  settlements: () => request('/settlements'),
  confirmSettlement: (id) => write(`/settlements/${encodeURIComponent(id)}/confirm`)
}

export const venueApi = {
  venues: () => request('/venues'),
  sessions: () => request('/sessions?page=1&pageSize=50'),
  session: (id) => request(`/sessions/${encodeURIComponent(id)}`),
  events: (id) => request(`/sessions/${encodeURIComponent(id)}/events`),
  createSession: (payload) => write('/sessions', payload),
  publish: (id) => write(`/sessions/${encodeURIComponent(id)}/publish`, { actor: '许汝林' }),
  sell: (id, quantity) => write(`/sessions/${encodeURIComponent(id)}/sell`, { quantity, actor: '许汝林' }),
  checkin: (id, ticketCode) => write(`/sessions/${encodeURIComponent(id)}/checkin`, { ticketCode, actor: '现场工作人员' }),
  advance: (id, status) => write(`/sessions/${encodeURIComponent(id)}/status`, { status, actor: '许汝林' }),
  settle: (id) => write(`/sessions/${encodeURIComponent(id)}/settle`, { actor: '许汝林' })
}

export { API_BASE }
