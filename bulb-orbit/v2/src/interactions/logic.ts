import { getModel } from '../data/models'

const CORE_CYAN = '#00f0ff'

export function coreColorFor(id: string | null): string {
  if (!id) return CORE_CYAN
  return getModel(id)?.brandColor ?? CORE_CYAN
}

export interface HoverResult { modelId: string | null; changed: boolean }

export function resolveHover(prevId: string | null, hitId: string | null): HoverResult {
  return { modelId: hitId, changed: prevId !== hitId }
}
