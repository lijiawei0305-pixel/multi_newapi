import { type Model, DEFAULT_MODEL, getModel } from '../data/models'

export interface HudEls {
  name: HTMLElement
  provider: HTMLElement
  desc: HTMLElement
  telemetry: HTMLElement
  panel: HTMLElement
}

export function renderHud(els: HudEls, model: Model): void {
  els.name.textContent = model.name
  els.provider.textContent = model.provider
  els.desc.textContent = model.desc
  els.telemetry.textContent = model.telemetry
  els.panel.style.borderLeftColor = model.brandColor
}

export function hudModelFor(id: string | null): Model {
  if (!id) return DEFAULT_MODEL
  return getModel(id) ?? DEFAULT_MODEL
}
