export type Breakpoint = 'desktop' | 'tablet' | 'mobile'

export function breakpointFor(width: number): Breakpoint {
  if (width >= 1024) return 'desktop'
  if (width >= 768) return 'tablet'
  return 'mobile'
}

export function particleCount(bp: Breakpoint): number {
  return bp === 'mobile' ? 20000 : 40000
}

export function bloomEnabled(bp: Breakpoint): boolean {
  return bp !== 'mobile'
}
