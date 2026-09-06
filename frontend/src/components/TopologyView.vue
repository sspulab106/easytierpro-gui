<script setup lang="ts">
import { computed } from 'vue'
import { useTheme } from '@/composables/useTheme'

interface TopologyPeer {
  id: string
  hostname: string
  ipv4: string
  tunnel: string
  latency: string
  loss: string
  nat: string
  cost: string
  isSelf: boolean
  isRelay: boolean
  proxyCidrs: string[]
}

const props = defineProps<{
  localHostname: string
  localIp: string
  localPeerId: string
  peers: TopologyPeer[]
  /** Peer id to highlight (driven by an external table selection). */
  highlightId?: string
}>()

const emit = defineEmits<{
  /** Fired when a node is clicked; '' when the background is clicked (clears). */
  (e: 'select', peerId: string): void
}>()

function onNodeClick(id: string) {
  emit('select', id)
}

const { theme } = useTheme()
const isDark = computed(() => theme.value === 'dark')

// ---- theme colours ----
const ink = computed(() => (isDark.value ? '#D9D5C9' : '#111111'))
const labelColor = computed(() => (isDark.value ? '#D9D5C9' : '#3D3A30'))
const subColor = computed(() => (isDark.value ? '#8A857A' : '#7A7565'))
const nodeBg = computed(() => (isDark.value ? '#191713' : '#E8E4D9'))
const nodeText = computed(() => (isDark.value ? '#E8E4D9' : '#111111'))

const p2pColor = '#FF3B00'
const relayColor = '#2438FF'
const relayColorDark = '#4A5AFF'

function nodeColor(i: number): string {
  const palette = ['#FF3B00', '#2438FF', '#FFE600', '#00C200', '#FF2E2E', '#8B5CF6', '#06B6D4', '#FF8A66']
  return palette[i % palette.length]
}

// ---- layout: local centre, P2P left column, Relay right column ----
const VW = 760, VH = 520
const CX = 380, CY = 270
const P2P_X = 170, RELAY_X = 610, SAT_X = 700
const Y_TOP = 120, Y_BOT = 420

function yPositions(n: number): number[] {
  if (n < 2) return [CY]
  const step = (Y_BOT - Y_TOP) / (n - 1)
  return Array.from({ length: n }, (_, i) => Y_TOP + step * i)
}

const layout = computed(() => {
  const peers = props.peers ?? []
  const self = peers.find((p) => p.isSelf)
  const relays = peers.filter((p) => p.isRelay)
  const isRelayed = (p: TopologyPeer) => !p.isSelf && !p.isRelay && String(p.cost).startsWith('relay')
  const relayed = peers.filter(isRelayed)
  const direct = peers.filter((p) => !p.isSelf && !p.isRelay && !isRelayed(p))

  // Sun (local node)
  const sun = {
    peer: self ?? { id: props.localPeerId, hostname: props.localHostname, ipv4: props.localIp, tunnel: '-', latency: '-', loss: '-', nat: '-', cost: 'Local', isSelf: true, isRelay: false, proxyCidrs: [] },
    x: CX, y: CY, size: 56, kind: 'sun' as const,
  }

  // P2P column (left)
  const p2pNodes = direct.map((p, i) => {
    const ys = yPositions(direct.length)
    return { peer: p, x: P2P_X, y: ys[i], size: 44, kind: 'p2p' as const, color: nodeColor(i) }
  })

  // Relay column (right) + satellites
  const relayNodes: Array<{ peer: TopologyPeer; x: number; y: number; size: number; kind: 'relay' | 'satellite'; color: string; parent?: TopologyPeer }> = []
  const relayYs = yPositions(relays.length)

  // assign satellites to relays round-robin
  let satIdx = 0
  relays.forEach((p, i) => {
    relayNodes.push({ peer: p, x: RELAY_X, y: relayYs[i], size: 48, kind: 'relay', color: relayColor })
  })
  relayed.forEach((p) => {
    const parent = relays[satIdx % relays.length]
    const parentY = relayYs[satIdx % relays.length] // approximate
    const offset = (Math.floor(satIdx / relays.length) - (Math.floor(relayed.length / relays.length) / 2) + 0.5) * 36
    relayNodes.push({ peer: p, x: SAT_X, y: parentY + offset, size: 34, kind: 'satellite', color: subColor.value, parent })
    satIdx++
  })
  // if no relays but relayed exist, place them on the right column as plain nodes
  if (relays.length === 0 && relayed.length > 0) {
    const ys = yPositions(relayed.length)
    relayed.forEach((p, i) => {
      relayNodes.push({ peer: p, x: RELAY_X, y: ys[i], size: 40, kind: 'relay', color: subColor.value })
    })
  }

  // Build links: sun ↔ p2p, sun ↔ relay, relay ↔ satellite
  const links: Array<{ from: { x: number; y: number }; to: { x: number; y: number }; kind: 'p2p' | 'relay' }> = []
  p2pNodes.forEach((n) => links.push({ from: sun, to: n, kind: 'p2p' }))
  relayNodes.filter((n) => n.kind === 'relay').forEach((n) => links.push({ from: sun, to: n, kind: 'relay' }))
  relayNodes.filter((n) => n.kind === 'satellite').forEach((n) => {
    const parent = relayNodes.find((r) => r.kind === 'relay' && r.peer.id === n.parent?.id)
    if (parent) {
      links.push({ from: parent, to: n, kind: 'relay' })
    }
  })

  return { sun, p2pNodes, relayNodes, links, hasAny: peers.length > 0, hasPeer: direct.length + relays.length + relayed.length > 0 }
})

function fmtTunnel(t: string): string {
  return t && t !== '-' ? t.toUpperCase() : ''
}

function rect(cx: number, cy: number, size: number) {
  return { x: cx - size / 2, y: cy - size / 2, width: size, height: size }
}

// Selection highlight: peers table and topology share one selected id.
function isHl(id: string): boolean {
  return !!props.highlightId && props.highlightId === id
}

function hlStroke(id: string, base: string): string {
  return isHl(id) ? '#FFE600' : base
}

function hlWidth(id: string, base: number): number {
  return isHl(id) ? base + 2.5 : base
}
</script>

<template>
  <div @click.self="onNodeClick('')">
    <svg :viewBox="`0 0 ${VW} ${VH}`" class="w-full topology-svg" @click.self="onNodeClick('')">
      <!-- Group labels -->
      <text v-if="layout.p2pNodes.length" x="170" y="80" text-anchor="middle" font-size="10" font-weight="bold" :fill="ink" font-family="'Courier New',monospace" letter-spacing="2" class="grp-label">P2P DIRECT</text>
      <text v-if="layout.relayNodes.length" x="610" y="80" text-anchor="middle" font-size="10" font-weight="bold" :fill="ink" font-family="'Courier New',monospace" letter-spacing="2" class="grp-label">RELAY</text>

      <!-- Links: p2p solid, relay dashed -->
      <g v-for="(l, i) in layout.links" :key="'l' + i" class="link-line" :style="{ animationDelay: i * 30 + 'ms' }">
        <line
          :x1="l.from.x" :y1="l.from.y" :x2="l.to.x" :y2="l.to.y"
          :stroke="l.kind === 'p2p' ? p2pColor : relayColor"
          stroke-width="2"
          :stroke-dasharray="l.kind === 'p2p' ? 'none' : '6 5'"
          :stroke-opacity="l.kind === 'p2p' ? 0.85 : 0.6"
        />
        <!-- midpoint label -->
        <text
          :x="(l.from.x + l.to.x) / 2" :y="(l.from.y + l.to.y) / 2"
          text-anchor="middle" font-size="8" font-weight="bold"
          :fill="l.kind === 'p2p' ? p2pColor : relayColor"
          font-family="'Courier New',monospace"
          class="link-label"
        >{{ l.kind === 'p2p' ? 'P2P' : 'RELAY' }}</text>
      </g>

      <!-- Sun (local node) -->
      <g class="anim-node" style="animation-delay:0ms">
        <rect v-bind="rect(layout.sun.x, layout.sun.y, layout.sun.size)" :fill="nodeBg" :stroke="p2pColor" stroke-width="3" />
        <text :x="layout.sun.x" :y="layout.sun.y + 4" text-anchor="middle" font-size="12" font-weight="bold" :fill="nodeText" font-family="'Courier New',monospace">LOCAL</text>
        <text :x="layout.sun.x" :y="layout.sun.y + 62" text-anchor="middle" font-size="12" font-weight="bold" :fill="labelColor">{{ layout.sun.peer.hostname }}</text>
        <text :x="layout.sun.x" :y="layout.sun.y + 77" text-anchor="middle" font-size="10" :fill="subColor" font-family="'Courier New',monospace">{{ layout.sun.peer.ipv4 || 'DHCP' }}</text>
      </g>

      <!-- P2P nodes -->
      <g v-for="(n, i) in layout.p2pNodes" :key="'p' + i" class="anim-node cursor-pointer"
         :style="{ animationDelay: (i + 1) * 60 + 'ms' }"
         @click="onNodeClick(n.peer.id)">
        <rect v-bind="rect(n.x, n.y, n.size)" :fill="nodeBg" :fill-opacity="0.9"
              :stroke="hlStroke(n.peer.id, n.color)" :stroke-width="hlWidth(n.peer.id, 2.5)" />
        <text :x="n.x" :y="n.y + 5" text-anchor="middle" font-size="9" font-weight="bold" :fill="nodeText" font-family="'Courier New',monospace">{{ n.peer.hostname && n.peer.hostname !== '-' ? n.peer.hostname.slice(0, 12) : n.peer.id.slice(0, 8) }}</text>
        <text :x="n.x" :y="n.y + 52" text-anchor="middle" font-size="8" :fill="subColor" font-family="'Courier New',monospace">{{ n.peer.ipv4 || 'DHCP' }}</text>
        <text :x="n.x" :y="n.y + 64" text-anchor="middle" font-size="8" :fill="subColor" font-family="'Courier New',monospace">{{ n.peer.latency !== '-' ? n.peer.latency + 'ms' : '' }}{{ fmtTunnel(n.peer.tunnel) ? ' · ' + fmtTunnel(n.peer.tunnel) : '' }}</text>
      </g>

      <!-- Relay + satellite nodes -->
      <g v-for="(n, i) in layout.relayNodes" :key="'r' + i" class="anim-node cursor-pointer"
         :style="{ animationDelay: (layout.p2pNodes.length + i + 1) * 60 + 'ms' }"
         @click="onNodeClick(n.peer.id)">
        <rect
          v-bind="rect(n.x, n.y, n.size)"
          :fill="n.kind === 'relay' ? relayColor : nodeBg"
          :fill-opacity="n.kind === 'relay' ? 1 : 0.9"
          :stroke="hlStroke(n.peer.id, n.kind === 'relay' ? relayColor : n.color)"
          :stroke-width="hlWidth(n.peer.id, 2.5)"
        />
        <text
          v-if="n.kind === 'relay'"
          :x="n.x" :y="n.y + 4" text-anchor="middle" font-size="10" font-weight="bold" fill="#fff" font-family="'Courier New',monospace"
        >RELAY</text>
        <text
          :x="n.x" :y="n.y + (n.kind === 'relay' ? 56 : 44)" text-anchor="middle" font-size="9" font-weight="bold" :fill="n.kind === 'relay' ? labelColor : nodeText" font-family="'Courier New',monospace"
        >{{ n.peer.hostname && n.peer.hostname !== '-' ? n.peer.hostname.slice(0, 14) : n.peer.id.slice(0, 8) }}</text>
        <text :x="n.x" :y="n.y + (n.kind === 'relay' ? 70 : 56)" text-anchor="middle" font-size="8" :fill="subColor" font-family="'Courier New',monospace">{{ n.peer.ipv4 || 'DHCP' }}</text>
        <text :x="n.x" :y="n.y + (n.kind === 'relay' ? 82 : 68)" text-anchor="middle" font-size="8" :fill="subColor" font-family="'Courier New',monospace">{{ n.peer.latency !== '-' ? n.peer.latency + 'ms' : '' }}{{ fmtTunnel(n.peer.tunnel) ? ' · ' + fmtTunnel(n.peer.tunnel) : '' }}</text>
      </g>

      <!-- Empty state -->
      <text v-if="!layout.hasPeer" :x="CX" :y="CY + 110" text-anchor="middle" font-size="11" :fill="subColor" font-family="'Courier New',monospace">
        No peers connected yet
      </text>
    </svg>

    <!-- Legend -->
    <div v-if="layout.hasPeer" class="flex items-center gap-5 px-2 pt-1 pb-1 text-[10px] font-bold uppercase tracking-wider" :style="{ color: subColor }">
      <span class="flex items-center gap-1.5">
        <span class="inline-block w-6 border-t-2" :style="{ borderColor: p2pColor }" />
        P2P Direct
      </span>
      <span class="flex items-center gap-1.5">
        <span class="inline-block w-6 border-t-2 border-dashed" :style="{ borderColor: relayColor }" />
        Relay
      </span>
      <span class="flex items-center gap-1.5">
        <span class="inline-block w-2.5 h-2.5" :style="{ background: relayColor, border: '2px solid ' + ink }" />
        Relay Node
      </span>
    </div>
  </div>
</template>

<style scoped>
/* Selected node pulse (topology <-> table shared selection) */
g.cursor-pointer rect {
  transition: stroke 0.15s ease, stroke-width 0.15s ease;
}
g.cursor-pointer:hover rect {
  filter: brightness(1.15);
}

/* Animated entrance for nodes */
.anim-node {
  opacity: 0;
  animation: nodeFadeIn 0.5s ease-out forwards;
}
@keyframes nodeFadeIn {
  from { opacity: 0; transform: translateY(10px); }
  to   { opacity: 1; transform: translateY(0); }
}

/* Fade-in for links */
.link-line {
  opacity: 0;
  animation: linkFadeIn 0.4s ease-out forwards;
}
@keyframes linkFadeIn {
  from { opacity: 0; }
  to   { opacity: 1; }
}

/* Group labels */
.grp-label {
  opacity: 0;
  animation: grpLabelIn 0.3s 0.1s ease-out forwards;
}
@keyframes grpLabelIn {
  from { opacity: 0; transform: translateY(-6px); }
  to   { opacity: 1; transform: translateY(0); }
}

/* Link label (P2P / RELAY) */
.link-label {
  opacity: 0;
  animation: linkLabelIn 0.5s 0.2s ease-out forwards;
}
@keyframes linkLabelIn {
  from { opacity: 0; }
  to   { opacity: 1; }
}
</style>