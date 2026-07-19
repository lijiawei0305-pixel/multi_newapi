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
  createEmptyModelMappingGuardrail,
  hasAdvancedSettingsValues,
  parseSettingsRecord,
} from './channel-mutate-model'

describe('channel mutate model', () => {
  test('treats untouched channel defaults as having no advanced settings', () => {
    assert.equal(hasAdvancedSettingsValues(CHANNEL_FORM_DEFAULT_VALUES), false)
    assert.equal(
      hasAdvancedSettingsValues({
        ...CHANNEL_FORM_DEFAULT_VALUES,
        proxy: 'socks5://proxy.internal:1080',
      }),
      true
    )
  })

  test('accepts only object-shaped channel settings metadata', () => {
    assert.deepEqual(parseSettingsRecord('{"last_check":123}'), {
      last_check: 123,
    })
    assert.deepEqual(parseSettingsRecord('[1,2,3]'), {})
    assert.deepEqual(parseSettingsRecord('{broken'), {})
  })

  test('creates an isolated empty mapping guardrail', () => {
    const first = createEmptyModelMappingGuardrail()
    const second = createEmptyModelMappingGuardrail()
    first.entries.push({ source: 'client-model', target: 'upstream-model' })

    assert.equal(second.entries.length, 0)
    assert.equal(second.invalidJson, false)
  })
})
