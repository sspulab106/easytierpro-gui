<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from 'vue'

export interface BandSample {
  t: number // epoch ms
  rx: number // bytes/sec
  tx: number // bytes/sec
}

const props = defineProps<{ samples: BandSample[]; height?: number }>()

const canvas = ref<HTMLCanvasElement | null>(null)
const PX = window.devicePixelRatio || 1

function fmt(v: number): string {
  if (v >= 1024 * 1024) return (v / 1024 / 1024).toFixed(1) + ' MB/s'
  if (v >= 1024) return (v / 1024).toFixed(1) + ' KB/s'
  return Math.round(v) + ' B/s'
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

  const samples = props.samples
  if (samples.length < 2) {
    ctx.fillStyle = '#888'
    ctx.font = '11px monospace'
    ctx.fillText('…', 8, 16)
    return
  }

  const max = Math.max(1, ...samples.map((s) => Math.max(s.rx, s.tx)))
  const pad = 24
  const plotH = h - pad
  const step = w / (samples.length - 1)

  const line = (key: 'rx' | 'tx', color: string) => {
    ctx.strokeStyle = color
    ctx.lineWidth = 2
    ctx.beginPath()
    samples.forEach((s, i) => {
      const x = i * step
      const y = plotH - (s[key] / max) * plotH
      if (i === 0) ctx.moveTo(x, y)
      else ctx.lineTo(x, y)
    })
    ctx.stroke()
  }

  line('rx', '#FF4D1A')
  line('tx', '#2438FF')

  // labels
  ctx.fillStyle = '#888'
  ctx.font = '10px monospace'
  ctx.fillText('↓ ' + fmt(samples[samples.length - 1].rx), 8, h - 6)
  ctx.fillText('↑ ' + fmt(samples[samples.length - 1].tx), w / 2, h - 6)
  ctx.fillText('max ' + fmt(max), w - 70, h - 6)
}

function resize() {
  draw()
}

watch(() => props.samples, draw, { deep: true })
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
