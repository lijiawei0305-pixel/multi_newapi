const TAU = Math.PI * 2
const wrap = (a: number) => ((a % TAU) + TAU) % TAU

export function satelliteAngle(baseAngle: number, speed: number, t: number): number {
  return baseAngle + speed * t
}

export function precessionAngle(dir: 1 | -1, period: number, t: number): number {
  return wrap(dir * (TAU / period) * t)
}

export function evenAngles(n: number, phase: number): number[] {
  const step = TAU / n
  return Array.from({ length: n }, (_, i) => phase + i * step)
}

export function viewOffset(width: number, ratio: number): number {
  return width * ratio
}
