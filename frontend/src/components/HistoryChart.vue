<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from 'vue'

export interface HistoryPoint {
  ts: number // epoch seconds, bucket start
  rx: number // bytes in bucket
  tx: number // bytes in bucket
}

const props = defineProps<{ points: HistoryPoint[]; height?: number }>()

const canvas = ref<HTMLCanvasElement | null>(null)
const PX = window.devicePixelRatio || 1

function fmtBytes(v: number): string {
  if (v >= 1024 * 1024 * 1024) return (v / 1024 / 1024 / 1024).toFixed(2) + ' GB'
  if (v >= 1024 * 1024) return (v / 1024 / 1024).toFixed(1) + ' MB'
  if (v >= 1024) return (v / 1024).toFixed(1) + ' KB'
  return Math.round(v) + ' B'
}

function draw() {
  const el = canvas.value
  if (!el) return
  const w = el.clientWidth
  const h = props.height ?? 120
  if (w === 0) return
  el.width = w * PX
  el.height = h * PX
  const ctx = el.getContext('2d')
  if (!ctx) return
  ctx.scale(PX, PX)
  ctx.clearRect(0, 0, w, h)

  const pts = props.points
  if (pts.length < 2) {
    ctx.fillStyle = '#888'
    ctx.font = '11px monospace'
    ctx.fillText('…', 8, 16)
    return
  }

  const max = Math.max(1, ...pts.map((p) => Math.max(p.rx, p.tx)))
  const pad = 16
  const plotH = h - pad
  const step = w / (pts.length - 1)

  // soft area fill + line, mirroring the bandwidth chart's rx/tx colors
  const series = (key: 'rx' | 'tx', color: string) => {
    ctx.beginPath()
    pts.forEach((p, i) => {
      const x = i * step
      const y = plotH - (p[key] / max) * plotH
      if (i === 0) ctx.moveTo(x, y)
      else ctx.lineTo(x, y)
    })
    ctx.strokeStyle = color
    ctx.lineWidth = 1.5
    ctx.stroke()
    ctx.lineTo(w, plotH)
    ctx.lineTo(0, plotH)
    ctx.closePath()
    ctx.fillStyle = color.replace(')', ',0.08)').replace('rgb', 'rgba')
    ctx.fill()
  }
  series('rx', 'rgb(255,77,26)')
  series('tx', 'rgb(36,56,255)')

  // hour tick labels every 6 hours
  ctx.fillStyle = '#888'
  ctx.font = '9px monospace'
  pts.forEach((p, i) => {
    if (i % 6 !== 0) return
    const d = new Date(p.ts * 1000)
    ctx.fillText(String(d.getHours()).padStart(2, '0') + ':00', i * step, h - 4)
  })

  const last = pts[pts.length - 1]
  ctx.fillStyle = '#888'
  ctx.font = '10px monospace'
  ctx.fillText('↓ ' + fmtBytes(last.rx), 8, h - 16)
  ctx.fillText('↑ ' + fmtBytes(last.tx), w / 2 - 40, h - 16)
  ctx.fillText('max ' + fmtBytes(max), w - 64, h - 16)
}

function resize() {
  draw()
}

watch(() => props.points, draw, { deep: true })
onMounted(() => {
  draw()
  window.addEventListener('resize', resize)
})
onUnmounted(() => {
  window.removeEventListener('resize', resize)
})
</script>

<template>
  <canvas ref="canvas" class="w-full" :style="{ height: (height ?? 120) + 'px' }" />
</template>
