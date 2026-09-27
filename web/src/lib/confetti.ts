// Dependency-free confetti on a canvas styled through CSSOM (CSP-safe).

const COLORS = ['#ff4d8d', '#ffc83d', '#7c5cff', '#2ec5ff', '#34c759'] as const
const DURATION_MS = 1600

export function celebrate(): void {
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
  const count = document.documentElement.dataset.perf === 'low' ? 60 : 140
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  const w = window.innerWidth
  const h = window.innerHeight

  const canvas = document.createElement('canvas')
  Object.assign(canvas.style, { position: 'fixed', inset: '0', width: '100%', height: '100%', pointerEvents: 'none', zIndex: '1000' })
  canvas.width = w * dpr
  canvas.height = h * dpr
  canvas.setAttribute('aria-hidden', 'true')
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  document.body.append(canvas)
  ctx.scale(dpr, dpr)

  const particles = Array.from({ length: count }, () => ({
    x: w / 2,
    y: h * 0.35,
    vx: (Math.random() - 0.5) * 12,
    vy: -Math.random() * 12 - 4,
    rot: Math.random() * 6.3,
    vrot: (Math.random() - 0.5) * 0.3,
    size: 5 + Math.random() * 5,
    color: COLORS[Math.floor(Math.random() * COLORS.length)] ?? COLORS[0],
  }))

  const start = performance.now()
  const tick = (t: number) => {
    const k = (t - start) / DURATION_MS
    ctx.clearRect(0, 0, w, h)
    ctx.globalAlpha = Math.max(0, 1 - k)
    for (const p of particles) {
      p.vy += 0.35
      p.vx *= 0.99
      p.x += p.vx
      p.y += p.vy
      p.rot += p.vrot
      ctx.save()
      ctx.translate(p.x, p.y)
      ctx.rotate(p.rot)
      ctx.fillStyle = p.color
      ctx.fillRect(-p.size / 2, -p.size / 4, p.size, p.size / 2)
      ctx.restore()
    }
    if (k < 1) requestAnimationFrame(tick)
    else canvas.remove()
  }
  requestAnimationFrame(tick)
}
