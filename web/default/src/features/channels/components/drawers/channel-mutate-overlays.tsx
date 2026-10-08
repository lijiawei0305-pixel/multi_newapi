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
import type { ComponentProps } from 'react'

import { SecureVerificationDialog } from '@/features/auth/secure-verification'

import { AdvancedCustomEditorDialog } from '../dialogs/advanced-custom-editor-dialog'
import { FetchModelsDialog } from '../dialogs/fetch-models-dialog'
import {
  MissingModelsConfirmationDialog,
  type MissingModelsAction,
} from '../dialogs/missing-models-confirmation-dialog'
import { ParamOverrideEditorDialog } from '../dialogs/param-override-editor-dialog'
import { StatusCodeRiskDialog } from '../dialogs/status-code-risk-dialog'

type SecureVerificationDialogProps = ComponentProps<
  typeof SecureVerificationDialog
>

export function ChannelMutateOverlays(props: {
  sensitiveLocked: boolean
  paramOverrideEditorOpen: boolean
  paramOverrideValue: string
  onParamOverrideOpenChange: (open: boolean) => void
  onSaveParamOverride: (nextValue: string) => void
  advancedCustomEditorOpen: boolean
  advancedCustomValue: string
  onAdvancedCustomOpenChange: (open: boolean) => void
  onSaveAdvancedCustom: (nextValue: string) => void
  fetchModelsDialogOpen: boolean
  onFetchModelsOpenChange: (open: boolean) => void
  onModelsSelected: (models: string[]) => void
  redirectModels: string[]
  redirectSourceModels: string[]
  customFetcher?: () => Promise<string[]>
  channelName?: string
  existingModelsOverride?: string[]
  verification: SecureVerificationDialogProps
  missingModelsDialogOpen: boolean
  missingModels: string[]
  onMissingModelsAction: (action: MissingModelsAction) => void
  onMissingModelsOpenChange: (open: boolean) => void
  statusCodeRiskOpen: boolean
  statusCodeRiskDetailItems: string[]
  onStatusCodeRiskOpenChange: (open: boolean) => void
  onStatusCodeRiskConfirm: () => void
}) {
  return (
    <>
      {props.paramOverrideEditorOpen && !props.sensitiveLocked && (
        <ParamOverrideEditorDialog
          open={props.paramOverrideEditorOpen}
          value={props.paramOverrideValue}
          onOpenChange={props.onParamOverrideOpenChange}
          onSave={props.onSaveParamOverride}
        />
      )}

      {props.advancedCustomEditorOpen && !props.sensitiveLocked && (
        <AdvancedCustomEditorDialog
          open={props.advancedCustomEditorOpen}
          value={props.advancedCustomValue}
          onOpenChange={props.onAdvancedCustomOpenChange}
          onSave={props.onSaveAdvancedCustom}
        />
      )}

      <FetchModelsDialog
        open={props.fetchModelsDialogOpen}
        onOpenChange={props.onFetchModelsOpenChange}
        onModelsSelected={props.onModelsSelected}
        redirectModels={props.redirectModels}
        redirectSourceModels={props.redirectSourceModels}
        customFetcher={props.customFetcher}
        channelName={props.channelName}
        existingModelsOverride={props.existingModelsOverride}
      />

      <SecureVerificationDialog {...props.verification} />

      <MissingModelsConfirmationDialog
        open={props.missingModelsDialogOpen}
        missingModels={props.missingModels}
        onConfirm={props.onMissingModelsAction}
        onOpenChange={props.onMissingModelsOpenChange}
      />

      <StatusCodeRiskDialog
        open={props.statusCodeRiskOpen}
        onOpenChange={props.onStatusCodeRiskOpenChange}
        detailItems={props.statusCodeRiskDetailItems}
        onConfirm={props.onStatusCodeRiskConfirm}
      />
    </>
  )
}
