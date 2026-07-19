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

import {
  buildOperationsJson,
  parseInitialState,
  parsePruneObjectsDraft,
  parseReturnErrorDraft,
} from './param-override-editor-model'

const translate = (key: string) => key

describe('parameter override editor model', () => {
  test('round-trips operation rules without dropping explicit zero values', () => {
    const input = {
      operations: [
        {
          description: 'Set a deterministic temperature',
          mode: 'set',
          path: 'temperature',
          value: 0,
          keep_origin: true,
          conditions: [{ path: 'model', mode: 'prefix', value: 'openai/' }],
          logic: 'AND',
        },
      ],
    }

    const state = parseInitialState(JSON.stringify(input))
    const output = buildOperationsJson(
      state.operations,
      { validate: true },
      translate
    )

    assert.equal(state.editMode, 'visual')
    assert.equal(state.visualMode, 'operations')
    assert.deepEqual(JSON.parse(output), input)
  })

  test('normalizes structured return errors for the dedicated editor', () => {
    assert.deepEqual(
      parseReturnErrorDraft(
        JSON.stringify({
          status_code: 429,
          code: 'rate_limit',
          type: 'requests',
          message: 'Try again later',
        })
      ),
      {
        statusCode: 429,
        code: 'rate_limit',
        type: 'requests',
        message: 'Try again later',
        skipRetry: true,
        simpleMode: false,
      }
    )
  })

  test('treats an empty prune-object value as an empty simple rule', () => {
    const draft = parsePruneObjectsDraft('')

    assert.equal(draft.simpleMode, true)
    assert.equal(draft.typeText, '')
    assert.equal(draft.logic, 'AND')
    assert.equal(draft.recursive, true)
    assert.equal(draft.rules.length, 0)
  })
})
