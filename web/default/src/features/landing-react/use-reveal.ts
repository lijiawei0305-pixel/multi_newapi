/* 滚动入场观察器 —— 移植自 bulb-orbit/index.html 脚本1（1180–1183）。
   元素滚入视口即加 .in（一次性，随后 unobserve）；不支持 IntersectionObserver 时直接加 .in 兜底。
   与原版差异：改为按 ref 逐元素观察（取代全局 querySelectorAll），并在 React 卸载时 disconnect。 */
import { useEffect, useRef } from 'react'

export function useReveal<T extends HTMLElement>(threshold = 0.15) {
  const ref = useRef<T>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (!('IntersectionObserver' in window)) {
      el.classList.add('in')
      return
    }
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) {
            e.target.classList.add('in')
            io.unobserve(e.target)
          }
        }
      },
      { threshold }
    )
    io.observe(el)
    return () => io.disconnect()
  }, [threshold])

  return ref
}
