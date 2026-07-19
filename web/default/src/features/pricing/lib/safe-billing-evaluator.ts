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
type BillingValue = number | boolean | string

type BillingStaticType = 'number' | 'boolean' | 'string'

type BillingNode =
  | { kind: 'literal'; value: BillingValue }
  | { kind: 'variable'; name: string }
  | { kind: 'unary'; operator: string; operand: BillingNode }
  | {
      kind: 'binary'
      operator: string
      left: BillingNode
      right: BillingNode
    }
  | {
      kind: 'conditional'
      condition: BillingNode
      whenTrue: BillingNode
      whenFalse: BillingNode
    }
  | { kind: 'call'; name: string; arguments: BillingNode[] }

type BillingToken = {
  kind: 'number' | 'string' | 'identifier' | 'symbol' | 'eof'
  text: string
  value?: BillingValue
  offset: number
}

const MAX_EXPRESSION_BYTES = 16 * 1024
const MAX_EXPRESSION_NODES = 2048
const MAX_EXPRESSION_DEPTH = 128

const BILLING_VARIABLES = new Set([
  'p',
  'c',
  'len',
  'cr',
  'cc',
  'cc1h',
  'img',
  'img_o',
  'ai',
  'ao',
])

const TWO_CHARACTER_SYMBOLS = new Set(['&&', '||', '<=', '>=', '==', '!='])
const ONE_CHARACTER_SYMBOLS = new Set([
  '+',
  '-',
  '*',
  '/',
  '<',
  '>',
  '!',
  '?',
  ':',
  '(',
  ')',
  ',',
])

class BillingLexer {
  private offset = 0
  private tokenCount = 0

  constructor(private readonly input: string) {}

  next(): BillingToken {
    this.skipWhitespace()
    if (this.offset >= this.input.length) {
      return { kind: 'eof', text: '', offset: this.offset }
    }
    this.tokenCount += 1
    if (this.tokenCount > MAX_EXPRESSION_NODES * 4) {
      throw new Error('Expression has too many tokens')
    }

    const start = this.offset
    const first = this.input[this.offset]
    if (/[0-9.]/.test(first)) {
      const remainder = this.input.slice(this.offset)
      const match = remainder.match(/^(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?/)
      if (!match) {
        throw new Error(`Invalid number at offset ${start}`)
      }
      this.offset += match[0].length
      const value = Number(match[0])
      if (!Number.isFinite(value)) {
        throw new Error(`Number is not finite at offset ${start}`)
      }
      if (!/[.eE]/.test(match[0]) && !Number.isSafeInteger(value)) {
        throw new Error(
          `Integer is outside the safe preview range at offset ${start}`
        )
      }
      return { kind: 'number', text: match[0], value, offset: start }
    }

    if (first === '"') {
      return this.readString()
    }

    if (/[A-Za-z_]/.test(first)) {
      this.offset += 1
      while (
        this.offset < this.input.length &&
        /[A-Za-z0-9_]/.test(this.input[this.offset])
      ) {
        this.offset += 1
      }
      const text = this.input.slice(start, this.offset)
      return { kind: 'identifier', text, offset: start }
    }

    const twoCharacters = this.input.slice(this.offset, this.offset + 2)
    if (TWO_CHARACTER_SYMBOLS.has(twoCharacters)) {
      this.offset += 2
      return {
        kind: 'symbol',
        text: twoCharacters,
        offset: start,
      }
    }
    if (ONE_CHARACTER_SYMBOLS.has(first)) {
      this.offset += 1
      return { kind: 'symbol', text: first, offset: start }
    }
    throw new Error(`Unsupported character at offset ${start}`)
  }

  private skipWhitespace() {
    while (
      this.offset < this.input.length &&
      /\s/.test(this.input[this.offset])
    ) {
      this.offset += 1
    }
  }

  private readString(): BillingToken {
    const start = this.offset
    this.offset += 1
    let escaped = false
    while (this.offset < this.input.length) {
      const character = this.input[this.offset]
      this.offset += 1
      if (escaped) {
        escaped = false
        continue
      }
      if (character === '\\') {
        escaped = true
        continue
      }
      if (character === '"') {
        const text = this.input.slice(start, this.offset)
        let value: unknown
        try {
          value = JSON.parse(text)
        } catch {
          throw new Error(`Invalid string at offset ${start}`)
        }
        if (typeof value !== 'string') {
          throw new Error(`Invalid string at offset ${start}`)
        }
        return { kind: 'string', text, value, offset: start }
      }
    }
    throw new Error(`Unterminated string at offset ${start}`)
  }
}

class BillingParser {
  private current: BillingToken
  private nodeCount = 0
  private depth = 0

  constructor(private readonly lexer: BillingLexer) {
    this.current = lexer.next()
  }

  parse(): BillingNode {
    const expression = this.parseConditional()
    if (this.current.kind !== 'eof') {
      throw new Error(`Unexpected token at offset ${this.current.offset}`)
    }
    return expression
  }

  private parseConditional(): BillingNode {
    const condition = this.parseLogicalOr()
    if (!this.consume('?')) return condition
    const whenTrue = this.parseNestedExpression()
    this.expect(':')
    const whenFalse = this.parseNestedExpression()
    return this.node({
      kind: 'conditional',
      condition,
      whenTrue,
      whenFalse,
    })
  }

  private parseLogicalOr(): BillingNode {
    let expression = this.parseLogicalAnd()
    while (this.consume('||')) {
      expression = this.node({
        kind: 'binary',
        operator: '||',
        left: expression,
        right: this.parseLogicalAnd(),
      })
    }
    return expression
  }

  private parseLogicalAnd(): BillingNode {
    let expression = this.parseEquality()
    while (this.consume('&&')) {
      expression = this.node({
        kind: 'binary',
        operator: '&&',
        left: expression,
        right: this.parseEquality(),
      })
    }
    return expression
  }

  private parseEquality(): BillingNode {
    let expression = this.parseComparison()
    while (this.current.text === '==' || this.current.text === '!=') {
      const operator = this.current.text
      this.advance()
      expression = this.node({
        kind: 'binary',
        operator,
        left: expression,
        right: this.parseComparison(),
      })
    }
    return expression
  }

  private parseComparison(): BillingNode {
    let expression = this.parseAdditive()
    while (['<', '<=', '>', '>='].includes(this.current.text)) {
      const operator = this.current.text
      this.advance()
      expression = this.node({
        kind: 'binary',
        operator,
        left: expression,
        right: this.parseAdditive(),
      })
    }
    return expression
  }

  private parseAdditive(): BillingNode {
    let expression = this.parseMultiplicative()
    while (this.current.text === '+' || this.current.text === '-') {
      const operator = this.current.text
      this.advance()
      expression = this.node({
        kind: 'binary',
        operator,
        left: expression,
        right: this.parseMultiplicative(),
      })
    }
    return expression
  }

  private parseMultiplicative(): BillingNode {
    let expression = this.parseUnary()
    while (['*', '/'].includes(this.current.text)) {
      const operator = this.current.text
      this.advance()
      expression = this.node({
        kind: 'binary',
        operator,
        left: expression,
        right: this.parseUnary(),
      })
    }
    return expression
  }

  private parseUnary(): BillingNode {
    const operators: string[] = []
    while (['!', '+', '-'].includes(this.current.text)) {
      operators.push(this.current.text)
      if (operators.length > MAX_EXPRESSION_DEPTH) {
        throw new Error('Expression nesting is too deep')
      }
      this.advance()
    }
    let expression = this.parsePrimary()
    for (let index = operators.length - 1; index >= 0; index -= 1) {
      expression = this.node({
        kind: 'unary',
        operator: operators[index],
        operand: expression,
      })
    }
    return expression
  }

  private parsePrimary(): BillingNode {
    if (this.current.kind === 'number' || this.current.kind === 'string') {
      const value = this.current.value
      if (value === undefined) {
        throw new Error(`Invalid literal at offset ${this.current.offset}`)
      }
      this.advance()
      return this.node({ kind: 'literal', value })
    }
    if (this.current.kind === 'identifier') {
      const name = this.current.text
      this.advance()
      if (name === 'true' || name === 'false') {
        return this.node({ kind: 'literal', value: name === 'true' })
      }
      if (!this.consume('(')) {
        return this.node({ kind: 'variable', name })
      }
      const argumentsList: BillingNode[] = []
      if (!this.consume(')')) {
        do {
          argumentsList.push(this.parseNestedExpression())
        } while (this.consume(','))
        this.expect(')')
      }
      return this.node({ kind: 'call', name, arguments: argumentsList })
    }
    if (this.consume('(')) {
      const expression = this.parseNestedExpression()
      this.expect(')')
      return expression
    }
    throw new Error(`Expected expression at offset ${this.current.offset}`)
  }

  private parseNestedExpression(): BillingNode {
    this.depth += 1
    if (this.depth > MAX_EXPRESSION_DEPTH) {
      throw new Error('Expression nesting is too deep')
    }
    try {
      return this.parseConditional()
    } finally {
      this.depth -= 1
    }
  }

  private consume(text: string): boolean {
    if (this.current.text !== text) return false
    this.advance()
    return true
  }

  private expect(text: string) {
    if (!this.consume(text)) {
      throw new Error(`Expected ${text} at offset ${this.current.offset}`)
    }
  }

  private advance() {
    this.current = this.lexer.next()
  }

  private node<T extends BillingNode>(node: T): T {
    this.nodeCount += 1
    if (this.nodeCount > MAX_EXPRESSION_NODES) {
      throw new Error('Expression has too many operations')
    }
    return node
  }
}

type BillingEvaluation = {
  value: number
  matchedTier: string
}

class BillingRuntime {
  private matchedTier = ''

  constructor(private readonly variables: Readonly<Record<string, number>>) {}

  run(tree: BillingNode): BillingEvaluation {
    const value = requireNumber(this.evaluate(tree))
    if (!Number.isFinite(value)) {
      throw new Error('Expression result is not finite')
    }
    return { value, matchedTier: this.matchedTier }
  }

  private evaluate(node: BillingNode): BillingValue {
    switch (node.kind) {
      case 'literal':
        return node.value
      case 'variable':
        return this.evaluateVariable(node.name)
      case 'unary':
        return this.evaluateUnary(node)
      case 'binary':
        return this.evaluateBinary(node)
      case 'conditional':
        return requireBoolean(this.evaluate(node.condition))
          ? this.evaluate(node.whenTrue)
          : this.evaluate(node.whenFalse)
      case 'call':
        return this.evaluateCall(node)
    }
  }

  private evaluateVariable(name: string): number {
    if (!Object.hasOwn(this.variables, name)) {
      throw new Error(`Unknown variable: ${name}`)
    }
    const value = this.variables[name]
    if (!Number.isFinite(value)) {
      throw new Error(`Variable is not finite: ${name}`)
    }
    return value
  }

  private evaluateUnary(node: Extract<BillingNode, { kind: 'unary' }>) {
    const value = this.evaluate(node.operand)
    if (node.operator === '!') return !requireBoolean(value)
    const number = requireNumber(value)
    return node.operator === '-' ? -number : number
  }

  private evaluateBinary(
    node: Extract<BillingNode, { kind: 'binary' }>
  ): BillingValue {
    if (node.operator === '&&') {
      const left = requireBoolean(this.evaluate(node.left))
      return left ? requireBoolean(this.evaluate(node.right)) : false
    }
    if (node.operator === '||') {
      const left = requireBoolean(this.evaluate(node.left))
      return left ? true : requireBoolean(this.evaluate(node.right))
    }
    const left = this.evaluate(node.left)
    const right = this.evaluate(node.right)
    if (node.operator === '==') return left === right
    if (node.operator === '!=') return left !== right
    const leftNumber = requireNumber(left)
    const rightNumber = requireNumber(right)
    switch (node.operator) {
      case '+':
        return leftNumber + rightNumber
      case '-':
        return leftNumber - rightNumber
      case '*':
        return leftNumber * rightNumber
      case '/':
        if (rightNumber === 0) throw new Error('Division by zero')
        return leftNumber / rightNumber
      case '<':
        return leftNumber < rightNumber
      case '<=':
        return leftNumber <= rightNumber
      case '>':
        return leftNumber > rightNumber
      case '>=':
        return leftNumber >= rightNumber
      default:
        throw new Error(`Unsupported operator: ${node.operator}`)
    }
  }

  private evaluateCall(node: Extract<BillingNode, { kind: 'call' }>): number {
    if (node.name === 'tier') {
      if (node.arguments.length !== 2) {
        throw new Error('tier expects two arguments')
      }
      const name = this.evaluate(node.arguments[0])
      if (typeof name !== 'string') {
        throw new Error('tier name must be a string')
      }
      const value = requireNumber(this.evaluate(node.arguments[1]))
      this.matchedTier = name
      return value
    }
    const numericArguments = node.arguments.map((argument) =>
      requireNumber(this.evaluate(argument))
    )
    switch (node.name) {
      case 'max':
        requireArgumentCount(node.name, numericArguments, 2)
        return Math.max(numericArguments[0], numericArguments[1])
      case 'min':
        requireArgumentCount(node.name, numericArguments, 2)
        return Math.min(numericArguments[0], numericArguments[1])
      case 'abs':
        requireArgumentCount(node.name, numericArguments, 1)
        return Math.abs(numericArguments[0])
      case 'ceil':
        requireArgumentCount(node.name, numericArguments, 1)
        return Math.ceil(numericArguments[0])
      case 'floor':
        requireArgumentCount(node.name, numericArguments, 1)
        return Math.floor(numericArguments[0])
      default:
        throw new Error(`Unknown function: ${node.name}`)
    }
  }
}

export function evaluateSafeBillingExpression(
  expression: string,
  variables: Readonly<Record<string, number>>
): BillingEvaluation {
  if (new TextEncoder().encode(expression).byteLength > MAX_EXPRESSION_BYTES) {
    throw new Error('Expression is too long')
  }
  const normalizedExpression = normalizeVersion(expression)
  const tree = new BillingParser(new BillingLexer(normalizedExpression)).parse()
  const resultType = validateBillingTree(tree)
  if (resultType !== 'number') {
    throw new Error('Expression result must be a number')
  }
  return new BillingRuntime(variables).run(tree)
}

function normalizeVersion(expression: string): string {
  const version = expression.match(/^v(\d+):(.*)$/s)
  if (!version) return expression
  if (version[1] !== '1') {
    throw new Error(`Unsupported expression version: v${version[1]}`)
  }
  return version[2]
}

function validateBillingTree(node: BillingNode): BillingStaticType {
  switch (node.kind) {
    case 'literal': {
      const literalType = typeof node.value
      if (
        literalType === 'number' ||
        literalType === 'boolean' ||
        literalType === 'string'
      ) {
        return literalType
      }
      throw new Error('Unsupported literal type')
    }
    case 'variable':
      if (!BILLING_VARIABLES.has(node.name)) {
        throw new Error(`Unknown variable: ${node.name}`)
      }
      return 'number'
    case 'unary': {
      const operandType = validateBillingTree(node.operand)
      const expectedType = node.operator === '!' ? 'boolean' : 'number'
      requireStaticType(operandType, expectedType)
      return expectedType
    }
    case 'binary':
      return validateBinaryNode(node)
    case 'conditional': {
      requireStaticType(validateBillingTree(node.condition), 'boolean')
      const whenTrueType = validateBillingTree(node.whenTrue)
      const whenFalseType = validateBillingTree(node.whenFalse)
      if (whenTrueType !== whenFalseType) {
        throw new Error('Conditional branches must have the same type')
      }
      return whenTrueType
    }
    case 'call':
      return validateCallNode(node)
  }
}

function validateBinaryNode(
  node: Extract<BillingNode, { kind: 'binary' }>
): BillingStaticType {
  const leftType = validateBillingTree(node.left)
  const rightType = validateBillingTree(node.right)
  if (node.operator === '&&' || node.operator === '||') {
    requireStaticType(leftType, 'boolean')
    requireStaticType(rightType, 'boolean')
    return 'boolean'
  }
  if (node.operator === '==' || node.operator === '!=') {
    if (leftType !== rightType) {
      throw new Error('Equality operands must have the same type')
    }
    return 'boolean'
  }
  requireStaticType(leftType, 'number')
  requireStaticType(rightType, 'number')
  if (['<', '<=', '>', '>='].includes(node.operator)) return 'boolean'
  return 'number'
}

function validateCallNode(
  node: Extract<BillingNode, { kind: 'call' }>
): BillingStaticType {
  if (node.name === 'tier') {
    requireNodeArgumentCount(node, 2)
    requireStaticType(validateBillingTree(node.arguments[0]), 'string')
    requireStaticType(validateBillingTree(node.arguments[1]), 'number')
    return 'number'
  }

  const argumentCounts: Readonly<Record<string, number>> = {
    max: 2,
    min: 2,
    abs: 1,
    ceil: 1,
    floor: 1,
  }
  if (!Object.hasOwn(argumentCounts, node.name)) {
    throw new Error(`Unknown function: ${node.name}`)
  }
  requireNodeArgumentCount(node, argumentCounts[node.name])
  for (const argument of node.arguments) {
    requireStaticType(validateBillingTree(argument), 'number')
  }
  return 'number'
}

function requireNodeArgumentCount(
  node: Extract<BillingNode, { kind: 'call' }>,
  expected: number
) {
  if (node.arguments.length !== expected) {
    throw new Error(`${node.name} expects ${expected} arguments`)
  }
}

function requireStaticType(
  actual: BillingStaticType,
  expected: BillingStaticType
) {
  if (actual !== expected) {
    throw new Error(`Expected a ${expected} expression`)
  }
}

function requireNumber(value: BillingValue): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error('Expected a finite number')
  }
  return value
}

function requireBoolean(value: BillingValue): boolean {
  if (typeof value !== 'boolean') {
    throw new Error('Expected a boolean condition')
  }
  return value
}

function requireArgumentCount(
  name: string,
  values: BillingValue[],
  expected: number
) {
  if (values.length !== expected) {
    throw new Error(`${name} expects ${expected} arguments`)
  }
}
