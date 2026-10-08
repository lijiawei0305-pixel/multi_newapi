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
import { strict as assert } from 'node:assert'

import { describe, test } from 'vitest'

import { CHANNEL_FORM_DEFAULT_VALUES } from '../../lib'
import {
  deriveChannelEditorSections,
  type ChannelEditorSectionInput,
} from './channel-editor-status'

function sectionInput(
  overrides: Partial<ChannelEditorSectionInput> = {}
): ChannelEditorSectionInput {
  return {
    isEditing: false,
    currentName: CHANNEL_FORM_DEFAULT_VALUES.name,
    currentType: CHANNEL_FORM_DEFAULT_VALUES.type,
    currentKey: CHANNEL_FORM_DEFAULT_VALUES.key,
    currentBaseUrl: CHANNEL_FORM_DEFAULT_VALUES.base_url,
    currentOther: CHANNEL_FORM_DEFAULT_VALUES.other,
    modelCount: 0,
    groupCount: CHANNEL_FORM_DEFAULT_VALUES.group.length,
    identityHasErrors: false,
    credentialsHaveErrors: false,
    modelsHaveErrors: false,
    advancedHaveErrors: false,
    ...overrides,
  }
}

describe('channel editor create and edit progress', () => {
  test('a new channel stays incomplete until name, key, and models are provided', () => {
    const created = deriveChannelEditorSections(sectionInput())

    assert.equal(created.identityStatus, 'idle')
    assert.equal(created.credentialsStatus, 'idle')
    assert.equal(created.modelsStatus, 'idle')
    assert.equal(created.advancedStatus, 'idle')
    assert.equal(created.progressLabel, '0/3')
  })

  test('a new channel is ready when the three required sections are filled', () => {
    const created = deriveChannelEditorSections(
      sectionInput({
        currentName: 'Primary upstream',
        currentKey: 'sk-test',
        modelCount: 2,
      })
    )

    assert.equal(created.identityStatus, 'complete')
    assert.equal(created.credentialsStatus, 'complete')
    assert.equal(created.modelsStatus, 'complete')
    assert.equal(created.progressLabel, '3/3')
  })

  test('editing an existing channel does not require the key to be entered again', () => {
    const editing = deriveChannelEditorSections(
      sectionInput({
        isEditing: true,
        currentName: 'Primary upstream',
        currentKey: '',
        modelCount: 1,
      })
    )

    assert.equal(editing.credentialsStatus, 'complete')
    assert.equal(editing.progressLabel, '3/3')
  })

  test('providers that require a base URL and other stay incomplete until both are set', () => {
    const missing = deriveChannelEditorSections(
      sectionInput({
        currentName: 'Azure',
        currentType: 3,
        currentKey: 'sk-test',
        modelCount: 1,
      })
    )
    const ready = deriveChannelEditorSections(
      sectionInput({
        currentName: 'Azure',
        currentType: 3,
        currentKey: 'sk-test',
        currentBaseUrl: 'https://example.openai.azure.com',
        currentOther: '2024-10-21',
        modelCount: 1,
      })
    )

    assert.equal(missing.credentialsStatus, 'idle')
    assert.equal(missing.progressLabel, '2/3')
    assert.equal(ready.credentialsStatus, 'complete')
    assert.equal(ready.progressLabel, '3/3')
  })

  test('a validation error overrides an otherwise complete section', () => {
    const invalid = deriveChannelEditorSections(
      sectionInput({
        currentName: 'Primary upstream',
        currentKey: 'sk-test',
        modelCount: 1,
        identityHasErrors: true,
        advancedHaveErrors: true,
      })
    )

    assert.equal(invalid.identityStatus, 'error')
    assert.equal(invalid.advancedStatus, 'error')
    assert.equal(invalid.progressLabel, '3/3')
  })
})
