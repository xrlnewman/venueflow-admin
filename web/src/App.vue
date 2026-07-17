<script setup>
import { computed, onMounted, ref } from 'vue'
import { fleetApi } from './api'

const nav = [
  ['overview', '总览', '⌂'], ['shipments', '活动订单调度', '↗'], ['drivers', '场地资源场馆工作人员', '◉'],
  ['exceptions', '场地事件中心', '!'], ['settlements', '对账结算', '￥']
]
const active = ref('overview')
const toast = ref('')
const syncing = ref(false)
const live = ref(false)
const settlements = ref([])
const shipments = ref([
  { id: 'VN-260716-018', route: '会展中心 → A 厅', cargo: '品牌发布会 · 500 人', driver: '周协调员', vehicle: 'A 厅 · 500 人', eta: '14:35', status: '进行中', tone: 'blue' },
  { id: 'VN-260716-017', route: '艺术馆 → B 厅', cargo: '艺术展览 · 300 人', driver: '陈协调员', vehicle: 'B 厅 · 300 人', eta: '15:10', status: '已锁场', tone: 'orange' },
  { id: 'VN-260716-016', route: '城市公园 → 草坪', cargo: '音乐节 · 800 人', driver: '林协调员', vehicle: '草坪 · 800 人', eta: '已撤场', status: '已完成', tone: 'green' },
  { id: 'VN-260716-015', route: '文化中心 → 剧场', cargo: '年度论坛 · 600 人', driver: '王协调员', vehicle: '剧场 · 600 人', eta: '16:20', status: '待预订', tone: 'purple' },
  { id: 'VN-260716-014', route: '会展中心 → 会议室', cargo: '客户沙龙 · 80 人', driver: '赵协调员', vehicle: '会议室 · 80 人', eta: '17:05', status: '进行中', tone: 'blue' }
])
const exceptions = ref([
  { id: 'EX-041', type: '设备告警', text: 'VN-260716-018 主会场音响需要复检', level: '高', tone: 'red' },
  { id: 'EX-040', type: '布场确认', text: 'VN-260716-017 签到台物料尚未入场', level: '中', tone: 'orange' },
  { id: 'EX-039', type: '场地资源告警', text: 'VN-260716-016 需要确认延迟撤场服务', level: '低', tone: 'blue' }
])
const title = computed(() => nav.find((item) => item[0] === active.value)?.[1] || '总览')
function flash(message) { toast.value = message; setTimeout(() => { toast.value = '' }, 2200) }
function toneFor(status) { return ({ '进行中': 'blue', '已锁场': 'orange', '已完成': 'green', '待预订': 'purple', '待结算': 'blue', '已取消': 'red' })[status] || 'purple' }
async function refresh() {
  syncing.value = true
  try {
    const [shipmentData, exceptionData, settlementData] = await Promise.all([fleetApi.shipments(), fleetApi.exceptions(), fleetApi.settlements()])
    shipments.value = shipmentData.list.map((item) => ({ ...item, tone: toneFor(item.status) }))
    exceptions.value = exceptionData.list
    settlements.value = settlementData.list
    live.value = true
    flash('已同步线上数据')
  } catch (error) {
    live.value = false
    flash(`演示数据：${error.message}`)
  } finally { syncing.value = false }
}
async function assign(item) {
  try { const updated = await fleetApi.assignShipment(item.id, item.driver); Object.assign(item, updated, { tone: toneFor(updated.status) }); flash(`${item.id} 已分配给 ${item.driver}`); await refresh() }
  catch (error) { flash(error.message) }
}
async function advance(item) {
  const next = item.status === '进行中' ? '待结算' : item.status === '待结算' ? '已完成' : ''
  if (!next) { flash(`${item.id} 当前无需推进`); return }
  try { await fleetApi.advanceShipment(item.id, next); flash(`${item.id} 已更新为${next}`); await refresh() } catch (error) { flash(error.message) }
}
async function resolve(item) {
  try { await fleetApi.resolveException(item.id); flash(`${item.id} 已标记为已处理`); await refresh() } catch (error) { flash(error.message) }
}
async function createShipment() {
  const route = window.prompt('输入活动场地路线', '会展中心 → A 厅')
  if (!route) return
  const cargo = window.prompt('输入活动与容量摘要', '品牌发布会 · 500 人')
  if (!cargo) return
  try { await fleetApi.createShipment({ route, cargo, eta: '待安排' }); flash('新活动订单已创建，等待锁场'); await refresh(); active.value = 'shipments' } catch (error) { flash(error.message) }
}
async function confirmSettlement() {
  const pending = settlements.value.find((item) => item.status !== '已结算')
  if (!pending) { flash('暂无待确认结算单'); return }
  try { await fleetApi.confirmSettlement(pending.id); flash(`${pending.period} 结算已确认`); await refresh() } catch (error) { flash(error.message) }
}
onMounted(refresh)
</script>

<template>
  <div class="shell">
    <aside class="sidebar">
      <div class="brand"><span class="brand-mark">↗</span><div><strong>VenueFlow</strong><small>场馆运营中心</small></div></div>
      <div class="workspace"><span class="pulse"></span> 上海运营中心 <b>⌄</b></div>
      <p class="nav-caption">运营工作台</p>
      <nav><button v-for="item in nav" :key="item[0]" :class="['nav-item', { active: active === item[0] }]" @click="active = item[0]"><span>{{ item[2] }}</span>{{ item[1] }}<em v-if="item[0] === 'exceptions'">{{ exceptions.length }}</em></button></nav>
      <div class="sidebar-foot"><div class="avatar">许</div><div><strong>许汝林</strong><small>调度主管</small></div><button @click="flash('演示模式已保持登录')">⋯</button></div>
    </aside>
    <main class="main">
      <header class="topbar"><span>工作台　/　<strong>{{ title }}</strong></span><span class="top-meta">2026 年 7 月 16 日 · 星期四 <b :class="{ offline: !live }">● {{ live ? '线上数据' : '演示数据' }}</b></span></header>
      <section class="heading"><div><p class="eyebrow">THURSDAY, JUL 16 · VENUEFLOW</p><h1>{{ title }} <i>✦</i></h1><p class="intro">城市现场服务的每一公里，都应该可见、可控、可复盘。</p></div><div class="heading-actions"><button class="secondary" :disabled="syncing" @click="refresh">{{ syncing ? '同步中…' : '↻ 刷新' }}</button><button class="primary" @click="createShipment">＋ 新建活动订单</button></div></section>
      <template v-if="active === 'overview'">
        <section class="metrics"><article class="metric dark"><span>今日活动订单</span><strong>128</strong><small>↗ 较昨日 +18.4%</small></article><article class="metric"><span>准时达成率</span><strong>96.8<small>%</small></strong><div class="bar"><i style="width:96.8%"></i></div></article><article class="metric"><span>在途场地资源</span><strong>42<small> / 58</small></strong><small class="good">场地资源利用率 72%</small></article><article class="metric warm"><span>待处理场地事件</span><strong>{{ exceptions.length }}<small> 条</small></strong><small class="warn">需要今日闭环</small></article></section>
        <section class="grid-two"><article class="panel chart-panel"><div class="panel-head"><div><h2>现场服务履约趋势</h2><p>近 7 日活动订单完成与准时率</p></div><span class="legend"><b></b>完成单　<i></i>准时率</span></div><div class="chart"><div class="chart-labels"><span>140</span><span>100</span><span>60</span><span>20</span></div><div class="bars"><i style="height:55%"></i><i style="height:70%"></i><i style="height:63%"></i><i style="height:88%"></i><i style="height:78%"></i><i style="height:94%"></i><i class="today" style="height:82%"></i></div></div><div class="days"><span>周五</span><span>周六</span><span>周日</span><span>周一</span><span>周二</span><span>周三</span><span>今天</span></div></article><article class="panel route-panel"><div class="panel-head"><div><h2>实时线路</h2><p>现场服务节点实时状态</p></div><button class="link" @click="active = 'shipments'">查看调度 →</button></div><div class="route-map"><div class="map-grid"></div><span class="map-line line-a"></span><span class="map-line line-b"></span><b class="pin pin-a">18</b><b class="pin pin-b">31</b><b class="pin pin-c">06</b><div class="map-label"><strong>42</strong><span>辆车在途</span></div></div></article></section>
        <section class="grid-two lower"><article class="panel"><div class="panel-head"><div><h2>今日活动订单</h2><p>按预计送达时间排序</p></div><button class="link" @click="active = 'shipments'">全部活动订单 →</button></div><div class="shipment-list"><div v-for="item in shipments.slice(0, 4)" :key="item.id" class="shipment-row"><span class="status-dot" :class="item.tone"></span><div><strong>{{ item.route }}</strong><small>{{ item.id }} · {{ item.cargo }}</small></div><span class="driver">{{ item.driver }}<small>{{ item.vehicle }}</small></span><b :class="['pill', item.tone]">{{ item.status }}</b></div></div></article><article class="panel"><div class="panel-head"><div><h2>场地事件待处理</h2><p>超过 SLA 会自动升级</p></div><button class="link" @click="active = 'exceptions'">场地事件中心 →</button></div><div class="exception-list"><div v-for="item in exceptions" :key="item.id" class="exception-row"><span :class="['exception-icon', item.tone]">!</span><div><strong>{{ item.type }}</strong><small>{{ item.text }}</small></div><button class="resolve" @click="resolve(item)">处理</button></div><p v-if="!exceptions.length" class="empty">今日场地事件已全部闭环 ✦</p></div></article></section>
      </template>
      <section v-else-if="active === 'shipments'" class="panel full"><div class="panel-head"><div><h2>活动订单调度</h2><p>{{ shipments.length }} 条活动订单 · 创建、锁场、现场服务、结算全流程</p></div><button class="primary small" @click="createShipment">＋ 新建活动订单</button></div><div class="table"><div class="table-head"><span>活动订单 / 场地</span><span>活动与容量</span><span>场馆协调员 / 场地资源</span><span>预计开始</span><span>状态</span><span>动作</span></div><div v-for="item in shipments" :key="item.id" class="table-row"><span><strong>{{ item.id }}</strong><small>{{ item.route }}</small></span><span>{{ item.cargo }}</span><span>{{ item.driver || '待分配' }}<small>{{ item.vehicle || '—' }}</small></span><span>{{ item.eta }}</span><span><b :class="['pill', item.tone]">{{ item.status }}</b></span><span><button v-if="item.status === '待预订' || item.status === '已锁场'" class="text-action" @click="assign(item)">确认锁场</button><button v-else-if="item.status === '进行中' || item.status === '待结算'" class="text-action" @click="advance(item)">{{ item.status === '进行中' ? '提交结算' : '完成归档' }}</button><button v-else class="text-action muted-action" @click="flash(`${item.id} 已完成闭环`)">已闭环</button></span></div></div></section>
      <section v-else-if="active === 'drivers'" class="panel full"><div class="panel-head"><div><h2>场地资源场馆工作人员</h2><p>58 个接入场地资源 · 42 个正在使用</p></div><button class="link" @click="flash('场地资源导出任务已创建')">导出名单 ↓</button></div><div class="driver-grid"><article v-for="(driver, index) in ['周协调员','陈协调员','林协调员','王协调员','赵协调员','孙协调员']" :key="driver" class="driver-card"><div class="driver-avatar">{{ driver[0] }}</div><div><strong>{{ driver }}</strong><small>{{ ['A 厅 · 500 人','B 厅 · 300 人','草坪 · 800 人','剧场 · 600 人','会议室 · 80 人','露台 · 120 人'][index] }}</small></div><span :class="['online', index === 3 ? 'busy' : '']">{{ index === 3 ? '休息中' : '进行中' }}</span><b>{{ [98, 96, 94, 88, 97, 91][index] }}<small> 分</small></b></article></div></section>
      <section v-else-if="active === 'exceptions'" class="panel full"><div class="panel-head"><div><h2>场地事件中心</h2><p>按优先级处理现场服务中的场地事件事件</p></div><span class="filter-chip">全部场地事件　⌄</span></div><div class="exception-table"><div v-for="item in exceptions" :key="item.id" class="exception-card"><span :class="['exception-icon', item.tone]">!</span><div><strong>{{ item.id }} · {{ item.type }}</strong><p>{{ item.text }}</p><small>创建于 12 分钟前 · 自动规则提醒</small></div><button class="primary small" @click="resolve(item)">标记已处理</button></div><p v-if="!exceptions.length" class="empty">暂无待处理场地事件 ✦</p></div></section>
      <section v-else class="panel full"><div class="panel-head"><div><h2>对账结算</h2><p>本月现场服务服务费与场馆工作人员结算进度</p></div><button class="primary small" @click="confirmSettlement">确认待结算</button></div><div class="settlement-summary"><div><span>本月应付</span><strong>¥{{ settlements.reduce((sum, item) => sum + Number(item.amount || 0), 0).toLocaleString() }}</strong><small>数据来自结算服务</small></div><div><span>待确认结算单</span><strong>{{ settlements.filter((item) => item.status !== '已结算').length }}</strong><small>可确认后留痕</small></div><div><span>平均单价</span><strong>¥42.6</strong><small>较上月 +6.4%</small></div></div><div class="table compact"><div class="table-head"><span>结算周期</span><span>场馆工作人员数</span><span>活动订单数</span><span>金额</span><span>状态</span></div><div v-for="row in settlements" :key="row.id" class="table-row"><span><strong>{{ row.period }}</strong></span><span>{{ row.driverCount }}</span><span>{{ row.shipmentCount }}</span><span>¥{{ Number(row.amount).toLocaleString() }}</span><b :class="['pill', row.status === '已结算' ? 'green' : 'orange']">{{ row.status }}</b></div></div></section>
      <footer>VenueFlow 场馆运营中心 · 免费开源 · MySQL 8.4 + Redis 8 · 演示数据</footer>
      <div v-if="toast" class="toast">{{ toast }}</div>
    </main>
  </div>
</template>
