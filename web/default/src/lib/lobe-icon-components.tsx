/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { lazy, type ComponentType } from 'react'

type IconComponent = ComponentType<Record<string, unknown>>
type LazyIconProps = {
  name: string
  variant?: string
  iconProps: Record<string, string | number | boolean>
  fallbackName: string
  fallbackSize: number
}

// Keep arbitrary administrator-configured @lobehub/icons names working, but
// load the large namespace only when an icon is actually rendered. A single
// async namespace chunk avoids duplicating shared icon helpers across hundreds
// of per-brand chunks.
const LazyIconNamespace = lazy(async () => {
  const icons = await import('@lobehub/icons')
  const LoadedIcon = ({
    name,
    variant,
    iconProps,
    fallbackName,
    fallbackSize,
  }: LazyIconProps) => {
    const baseIcon = (icons as Record<string, unknown>)[name] as
      | (IconComponent & Record<string, unknown>)
      | undefined
    if (!baseIcon) {
      return <LobeIconFallback name={fallbackName} size={fallbackSize} />
    }
    const candidate = variant ? baseIcon[variant] : baseIcon
    const selected =
      candidate &&
      (typeof candidate === 'function' || typeof candidate === 'object')
        ? (candidate as IconComponent)
        : baseIcon
    const SelectedIcon = selected
    return <SelectedIcon {...iconProps} />
  }
  return { default: LoadedIcon }
})

export function LobeIconFallback({
  name,
  size,
}: {
  name: string
  size: number
}) {
  return (
    <div
      className='bg-muted text-muted-foreground flex items-center justify-center rounded-full text-xs font-medium'
      style={{ width: size, height: size }}
    >
      {name.charAt(0).toUpperCase() || '?'}
    </div>
  )
}

export function LobeIconRenderer({
  name,
  variant,
  iconProps,
  fallbackName,
  fallbackSize,
}: {
  name: string
  variant?: string
  iconProps: Record<string, string | number | boolean>
  fallbackName: string
  fallbackSize: number
}) {
  return (
    <LazyIconNamespace
      name={name}
      variant={variant}
      iconProps={iconProps}
      fallbackName={fallbackName}
      fallbackSize={fallbackSize}
    />
  )
}
