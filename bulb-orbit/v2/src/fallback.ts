// ==========================================
// WebGL 兜底 —— 检测当前环境是否具备可用的 WebGL 上下文；不具备时（或 URL 强制 ?nowebgl）
// 由 main.ts 调用 showPoster() 隐藏画布、注入静态海报图，跳过 mountScene()。
// win/doc 可注入，便于单测桩掉真实 window.WebGLRenderingContext / document.createElement。
// ==========================================

export function webglAvailable(
  win: { WebGLRenderingContext?: unknown } = window,
  doc: Document = document,
): boolean {
  try {
    if (!win.WebGLRenderingContext) return false
    const c = doc.createElement('canvas')
    return !!(c.getContext('webgl') || c.getContext('experimental-webgl'))
  } catch { return false }
}

export function showPoster(root: HTMLElement, src: string): void {
  const canvas = root.querySelector('#webgl-canvas')
  if (canvas instanceof HTMLElement) canvas.style.display = 'none'
  const img = document.createElement('img')
  img.src = src
  img.alt = 'WeDream AI'
  img.className = 'poster'
  root.appendChild(img)
}
