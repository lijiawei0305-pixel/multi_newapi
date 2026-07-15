import { test, expect } from 'bun:test'

import { nextSelection } from './scene3d-interaction'

test('命中即锁定;不同命中即切换', () => {
  expect(nextSelection(null, 'openai', true)).toBe('openai')
  expect(nextSelection('openai', 'gemini', true)).toBe('gemini')
})

test('命中空处(仍在画布内)保持锁定,不松手', () => {
  expect(nextSelection('openai', null, true)).toBe('openai')
})

test('离开画布即清空(即便同时命中也忽略)', () => {
  expect(nextSelection('openai', null, false)).toBe(null)
  expect(nextSelection('openai', 'gemini', false)).toBe(null)
})
