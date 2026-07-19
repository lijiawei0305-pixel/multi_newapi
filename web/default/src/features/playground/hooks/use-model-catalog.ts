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
import { useMemo } from 'react'

import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { filterBySearch } from '@/features/pricing/lib/filters'

import { filterByCapability } from '../lib/capabilities'
import type { CatalogFilter } from '../types'

interface UseModelCatalogOptions {
  /** When non-null, keep only models whose `enable_groups` includes this group. */
  group: string | null
  /** Capability chip filter — 'all' | 'chat' | 'image' | 'video' */
  filter: CatalogFilter
  /** Free-text search query */
  search: string
}

/**
 * Derives a filtered + searched model list from the shared pricing data.
 *
 * Pipeline:
 *   usePricingData().models
 *     → group filter  (enable_groups.includes(group) when group is non-null)
 *     → filterByCapability(filter)
 *     → filterBySearch(search)
 *
 * `counts` is computed on the **group-filtered** set (before capability/search
 * filtering) so that the chip counts reflect how many models in the current
 * group belong to each capability bucket.
 */
export function useModelCatalog({
  group,
  filter,
  search,
}: UseModelCatalogOptions) {
  const { models: allModels, isLoading, error, refetch } = usePricingData()

  /** Step 1 — apply group filter */
  const groupFiltered = useMemo(() => {
    if (group === null) return allModels
    return allModels.filter((m) => m.enable_groups?.includes(group))
  }, [allModels, group])

  /** Counts per capability bucket, computed on the group-filtered set */
  const counts: Record<CatalogFilter, number> = useMemo(
    () => ({
      all: groupFiltered.length,
      chat: filterByCapability(groupFiltered, 'chat').length,
      image: filterByCapability(groupFiltered, 'image').length,
      video: filterByCapability(groupFiltered, 'video').length,
    }),
    [groupFiltered]
  )

  /** Step 2 — apply capability chip filter */
  const capFiltered = useMemo(
    () => filterByCapability(groupFiltered, filter),
    [groupFiltered, filter]
  )

  /** Step 3 — apply free-text search */
  const models = useMemo(
    () => filterBySearch(capFiltered, search),
    [capFiltered, search]
  )

  return { models, isLoading, error, counts, refetch }
}
