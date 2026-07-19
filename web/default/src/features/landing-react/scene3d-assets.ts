/* WeDream 落地页统一 3D 场景 —— 资源/工具纯函数。
   纯函数 sampleAlphaToPoints / approach 由 scene3d-assets.test.ts 单测(bun test);
   svgToTexture / loadImageTexture / makeGlowTexture 依赖 DOM+three,靠 playwright 视觉验证。 */
import { CanvasTexture } from 'three'

/** 指数逼近缓动:每帧把 current 朝 target 靠拢,帧率无关。 */
export function approach(
  current: number,
  target: number,
  ease: number,
  dt: number
): number {
  return current + (target - current) * (1 - Math.exp(-ease * dt))
}

/** 从 alpha 通道采样归一化点云。透明像素跳过;不足则按可用不透明像素循环取。
    归一化到 [-0.5,0.5] 的 x/y(图像 y 向下 → 世界 y 向上);z = 小幅抖动补厚度。 */
export function sampleAlphaToPoints(
  img: { data: Uint8ClampedArray; width: number; height: number },
  count: number,
  zJitter: number,
  rng: () => number = Math.random
): Float32Array {
  const { data, width: w, height: h } = img
  const opaque: number[] = []
  for (let i = 0; i < w * h; i++) if (data[i * 4 + 3] > 40) opaque.push(i)
  if (opaque.length === 0) return new Float32Array(0)
  const out = new Float32Array(count * 3)
  for (let i = 0; i < count; i++) {
    const px = opaque[(i * 9973) % opaque.length] // 质数步长打散,避免逐行聚集
    const cx = px % w
    const cy = Math.floor(px / w)
    out[i * 3] = cx / w - 0.5
    out[i * 3 + 1] = 0.5 - cy / h
    out[i * 3 + 2] = (rng() * 2 - 1) * zJitter
  }
  return out
}

/** logo(内联 `<svg…>` 或 `<img src="…">`)→ 画到 size² 离屏 canvas 并返回。
    画一次,既做 billboard 纹理(CanvasTexture 包它)又采 alpha 生成光晕点云。 */
export function drawLogoCanvas(
  logo: string,
  size: number
): Promise<HTMLCanvasElement> {
  return new Promise((resolve, reject) => {
    const c = document.createElement('canvas')
    c.width = c.height = size
    const ctx = c.getContext('2d')
    if (!ctx) {
      reject(new Error('2D canvas is unavailable'))
      return
    }
    const image = new Image()
    let url = ''
    const trimmed = logo.trim()
    if (trimmed.startsWith('<img')) {
      const m = trimmed.match(/src="([^"]+)"/)
      if (!m) {
        reject(new Error('img 缺 src'))
        return
      }
      image.crossOrigin = 'anonymous'
      image.src = m[1]
    } else {
      // 内联 SVG 作为独立图片必须带 xmlns 命名空间(DOM 里隐式,blob 里必需);补 width/height 保内在尺寸。
      let svg = trimmed
      if (!/\bxmlns=/.test(svg)) {
        svg = svg.replace(/<svg/i, '<svg xmlns="http://www.w3.org/2000/svg"')
      }
      if (!/<svg[^>]*\bwidth=/.test(svg)) {
        svg = svg.replace(/<svg/i, `<svg width="${size}" height="${size}"`)
      }
      const blob = new Blob([svg], { type: 'image/svg+xml' })
      url = URL.createObjectURL(blob)
      image.src = url
    }
    image.addEventListener('load', () => {
      ctx.drawImage(image, 0, 0, size, size)
      if (url) URL.revokeObjectURL(url)
      resolve(c)
    })
    image.addEventListener('error', (error) => {
      if (url) URL.revokeObjectURL(url)
      reject(error)
    })
  })
}

/** 径向渐变辉光 sprite 贴图(移植 yun-reference/main.js:288-304)。 */
export function makeGlowTexture(colorHex: string): CanvasTexture {
  const c = document.createElement('canvas')
  c.width = c.height = 64
  const ctx = c.getContext('2d')
  if (!ctx) throw new Error('2D canvas is unavailable')
  const g = ctx.createRadialGradient(32, 32, 0, 32, 32, 32)
  g.addColorStop(0, colorHex)
  g.addColorStop(0.2, colorHex)
  g.addColorStop(1, 'rgba(0,0,0,0)')
  ctx.fillStyle = g
  ctx.fillRect(0, 0, 64, 64)
  return new CanvasTexture(c)
}
