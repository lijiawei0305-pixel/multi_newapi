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

export type ChannelEditorSectionStatus =
  | 'complete'
  | 'configured'
  | 'error'
  | 'idle'

const BASE_URL_REQUIRED_TYPES = new Set([3, 8, 36, 45])
const OTHER_REQUIRED_TYPES = new Set([3, 18, 21, 39, 41, 49])

export type ChannelEditorSectionInput = {
  isEditing: boolean
  currentName?: string
  currentType: number
  currentKey?: string
  currentBaseUrl?: string
  currentOther?: string
  modelCount: number
  groupCount: number
  identityHasErrors: boolean
  credentialsHaveErrors: boolean
  modelsHaveErrors: boolean
  advancedHaveErrors: boolean
}

export type ChannelEditorSectionState = {
  identityStatus: ChannelEditorSectionStatus
  credentialsStatus: ChannelEditorSectionStatus
  modelsStatus: ChannelEditorSectionStatus
  advancedStatus: ChannelEditorSectionStatus
  progressLabel: string
}

export function getCompletionStatus(
  hasErrors: boolean,
  isComplete: boolean
): ChannelEditorSectionStatus {
  if (hasErrors) return 'error'
  if (isComplete) return 'complete'
  return 'idle'
}

export function deriveChannelEditorSections(
  input: ChannelEditorSectionInput
): ChannelEditorSectionState {
  const providerRequiresBaseUrl = BASE_URL_REQUIRED_TYPES.has(input.currentType)
  const providerRequiresOther = OTHER_REQUIRED_TYPES.has(input.currentType)
  const identityComplete = Boolean(
    input.currentName?.trim() && input.currentType > 0
  )
  const credentialsComplete = Boolean(
    (input.isEditing || input.currentKey?.trim()) &&
    (!providerRequiresBaseUrl || input.currentBaseUrl?.trim()) &&
    (!providerRequiresOther || input.currentOther?.trim())
  )
  const modelsComplete = Boolean(input.modelCount > 0 && input.groupCount > 0)
  const requiredCompletedCount = [
    identityComplete,
    credentialsComplete,
    modelsComplete,
  ].filter(Boolean).length

  return {
    identityStatus: getCompletionStatus(
      input.identityHasErrors,
      identityComplete
    ),
    credentialsStatus: getCompletionStatus(
      input.credentialsHaveErrors,
      credentialsComplete
    ),
    modelsStatus: getCompletionStatus(input.modelsHaveErrors, modelsComplete),
    advancedStatus: input.advancedHaveErrors ? 'error' : 'idle',
    progressLabel: `${requiredCompletedCount}/3`,
  }
}
