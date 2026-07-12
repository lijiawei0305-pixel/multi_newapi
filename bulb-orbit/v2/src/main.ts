// ==========================================
// 入口 —— 装配全部落在 scene.ts 的 mountScene()；本文件只做必需 DOM 元素的存在性校验、WebGL 兜底
// 判定（URL ?nowebgl 强制降级，或 webglAvailable() 真实探测失败 → 展示静态海报、不挂载场景）、
// 按当前断点解析移动端粒子数/bloom 开关（Task 12：consume Task 3 的 layout.ts，不重新实现），调用
// mountScene，并把它返回的 onResize 接到 window resize 事件上。
// ==========================================
import { mountScene } from './scene/scene'
import { webglAvailable, showPoster } from './fallback'
import { breakpointFor, particleCount, bloomEnabled } from './ui/layout'

const canvas = document.getElementById('webgl-canvas')
if (!(canvas instanceof HTMLCanvasElement)) throw new Error('#webgl-canvas not found')

const HUD_IDS = ['hud-panel', 'hud-name', 'hud-provider', 'hud-desc', 'hud-telemetry'] as const
for (const id of HUD_IDS) {
  if (!document.getElementById(id)) throw new Error(`#${id} not found`)
}

const bp = breakpointFor(window.innerWidth)
// ?nowebgl：视觉门/截图用来强制走兜底路径，不依赖真的桩掉 window.WebGLRenderingContext；
// URLSearchParams#has 对不带值的裸参数（"?nowebgl"）也返回 true。
const forceFallback = new URLSearchParams(window.location.search).has('nowebgl')

// 不用顶层 await：Vite 生产构建的默认 esbuild target（chrome87/safari14 等）不支持顶层 await，
// 会在 build 阶段（而非 typecheck）报错——包一层 async 函数规避。canvas 显式作为参数传入（而不是
// 让 main() 直接闭包捕获外层 canvas）：TS 的控制流窄化不会跨越函数声明体传播（函数声明会被提升，
// 编译器无法证明调用一定发生在上面 instanceof 校验之后），闭包捕获拿到的仍是校验前的
// HTMLElement | null；显式传参则在调用处（校验之后的同一层作用域）取值，窄化后的
// HTMLCanvasElement 类型能正确传入。
async function main(canvasEl: HTMLCanvasElement): Promise<void> {
  if (forceFallback || !webglAvailable()) {
    showPoster(document.body, '/poster.png')
    return
  }
  const { onResize } = await mountScene(canvasEl, {
    particleCount: particleCount(bp),
    bloomEnabled: bloomEnabled(bp),
  })
  window.addEventListener('resize', onResize)
}

main(canvas).catch((err: unknown) => {
  console.error('Fatal bootstrap error:', err)
})
