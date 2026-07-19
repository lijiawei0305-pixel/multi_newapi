/*
Copyright (C) 2025 QuantumNous

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

const MAX_EXPRESSION_BYTES = 16 * 1024;
const MAX_EXPRESSION_NODES = 2048;
const MAX_EXPRESSION_DEPTH = 128;
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
]);
const TWO_CHARACTER_SYMBOLS = new Set(['&&', '||', '<=', '>=', '==', '!=']);
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
]);

class BillingLexer {
  constructor(input) {
    this.input = input;
    this.offset = 0;
    this.tokenCount = 0;
  }

  next() {
    this.skipWhitespace();
    if (this.offset >= this.input.length) {
      return { kind: 'eof', text: '', offset: this.offset };
    }
    this.tokenCount += 1;
    if (this.tokenCount > MAX_EXPRESSION_NODES * 4) {
      throw new Error('Expression has too many tokens');
    }

    const start = this.offset;
    const first = this.input[this.offset];
    if (/[0-9.]/.test(first)) {
      const match = this.input
        .slice(this.offset)
        .match(/^(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?/);
      if (!match) throw new Error(`Invalid number at offset ${start}`);
      this.offset += match[0].length;
      const value = Number(match[0]);
      if (!Number.isFinite(value)) {
        throw new Error(`Number is not finite at offset ${start}`);
      }
      if (!/[.eE]/.test(match[0]) && !Number.isSafeInteger(value)) {
        throw new Error(
          `Integer is outside the safe preview range at offset ${start}`,
        );
      }
      return { kind: 'number', text: match[0], value, offset: start };
    }

    if (first === '"') return this.readString();
    if (/[A-Za-z_]/.test(first)) {
      this.offset += 1;
      while (
        this.offset < this.input.length &&
        /[A-Za-z0-9_]/.test(this.input[this.offset])
      ) {
        this.offset += 1;
      }
      const text = this.input.slice(start, this.offset);
      return { kind: 'identifier', text, offset: start };
    }

    const twoCharacters = this.input.slice(this.offset, this.offset + 2);
    if (TWO_CHARACTER_SYMBOLS.has(twoCharacters)) {
      this.offset += 2;
      return { kind: 'symbol', text: twoCharacters, offset: start };
    }
    if (ONE_CHARACTER_SYMBOLS.has(first)) {
      this.offset += 1;
      return { kind: 'symbol', text: first, offset: start };
    }
    throw new Error(`Unsupported character at offset ${start}`);
  }

  skipWhitespace() {
    while (
      this.offset < this.input.length &&
      /\s/.test(this.input[this.offset])
    ) {
      this.offset += 1;
    }
  }

  readString() {
    const start = this.offset;
    this.offset += 1;
    let escaped = false;
    while (this.offset < this.input.length) {
      const character = this.input[this.offset];
      this.offset += 1;
      if (escaped) {
        escaped = false;
        continue;
      }
      if (character === '\\') {
        escaped = true;
        continue;
      }
      if (character === '"') {
        const text = this.input.slice(start, this.offset);
        let value;
        try {
          value = JSON.parse(text);
        } catch {
          throw new Error(`Invalid string at offset ${start}`);
        }
        if (typeof value !== 'string') {
          throw new Error(`Invalid string at offset ${start}`);
        }
        return { kind: 'string', text, value, offset: start };
      }
    }
    throw new Error(`Unterminated string at offset ${start}`);
  }
}

class BillingParser {
  constructor(lexer) {
    this.lexer = lexer;
    this.current = lexer.next();
    this.nodeCount = 0;
    this.depth = 0;
  }

  parse() {
    const expression = this.parseConditional();
    if (this.current.kind !== 'eof') {
      throw new Error(`Unexpected token at offset ${this.current.offset}`);
    }
    return expression;
  }

  parseConditional() {
    const condition = this.parseLogicalOr();
    if (!this.consume('?')) return condition;
    const whenTrue = this.parseNestedExpression();
    this.expect(':');
    const whenFalse = this.parseNestedExpression();
    return this.node({
      kind: 'conditional',
      condition,
      whenTrue,
      whenFalse,
    });
  }

  parseLogicalOr() {
    let expression = this.parseLogicalAnd();
    while (this.consume('||')) {
      expression = this.node({
        kind: 'binary',
        operator: '||',
        left: expression,
        right: this.parseLogicalAnd(),
      });
    }
    return expression;
  }

  parseLogicalAnd() {
    let expression = this.parseEquality();
    while (this.consume('&&')) {
      expression = this.node({
        kind: 'binary',
        operator: '&&',
        left: expression,
        right: this.parseEquality(),
      });
    }
    return expression;
  }

  parseEquality() {
    let expression = this.parseComparison();
    while (this.current.text === '==' || this.current.text === '!=') {
      const operator = this.current.text;
      this.advance();
      expression = this.node({
        kind: 'binary',
        operator,
        left: expression,
        right: this.parseComparison(),
      });
    }
    return expression;
  }

  parseComparison() {
    let expression = this.parseAdditive();
    while (['<', '<=', '>', '>='].includes(this.current.text)) {
      const operator = this.current.text;
      this.advance();
      expression = this.node({
        kind: 'binary',
        operator,
        left: expression,
        right: this.parseAdditive(),
      });
    }
    return expression;
  }

  parseAdditive() {
    let expression = this.parseMultiplicative();
    while (this.current.text === '+' || this.current.text === '-') {
      const operator = this.current.text;
      this.advance();
      expression = this.node({
        kind: 'binary',
        operator,
        left: expression,
        right: this.parseMultiplicative(),
      });
    }
    return expression;
  }

  parseMultiplicative() {
    let expression = this.parseUnary();
    while (['*', '/'].includes(this.current.text)) {
      const operator = this.current.text;
      this.advance();
      expression = this.node({
        kind: 'binary',
        operator,
        left: expression,
        right: this.parseUnary(),
      });
    }
    return expression;
  }

  parseUnary() {
    const operators = [];
    while (['!', '+', '-'].includes(this.current.text)) {
      operators.push(this.current.text);
      if (operators.length > MAX_EXPRESSION_DEPTH) {
        throw new Error('Expression nesting is too deep');
      }
      this.advance();
    }
    let expression = this.parsePrimary();
    for (let index = operators.length - 1; index >= 0; index -= 1) {
      expression = this.node({
        kind: 'unary',
        operator: operators[index],
        operand: expression,
      });
    }
    return expression;
  }

  parsePrimary() {
    if (this.current.kind === 'number' || this.current.kind === 'string') {
      const value = this.current.value;
      this.advance();
      return this.node({ kind: 'literal', value });
    }
    if (this.current.kind === 'identifier') {
      const name = this.current.text;
      this.advance();
      if (name === 'true' || name === 'false') {
        return this.node({ kind: 'literal', value: name === 'true' });
      }
      if (!this.consume('(')) return this.node({ kind: 'variable', name });
      const args = [];
      if (!this.consume(')')) {
        do {
          args.push(this.parseNestedExpression());
        } while (this.consume(','));
        this.expect(')');
      }
      return this.node({ kind: 'call', name, arguments: args });
    }
    if (this.consume('(')) {
      const expression = this.parseNestedExpression();
      this.expect(')');
      return expression;
    }
    throw new Error(`Expected expression at offset ${this.current.offset}`);
  }

  parseNestedExpression() {
    this.depth += 1;
    if (this.depth > MAX_EXPRESSION_DEPTH) {
      throw new Error('Expression nesting is too deep');
    }
    try {
      return this.parseConditional();
    } finally {
      this.depth -= 1;
    }
  }

  consume(text) {
    if (this.current.text !== text) return false;
    this.advance();
    return true;
  }

  expect(text) {
    if (!this.consume(text)) {
      throw new Error(`Expected ${text} at offset ${this.current.offset}`);
    }
  }

  advance() {
    this.current = this.lexer.next();
  }

  node(node) {
    this.nodeCount += 1;
    if (this.nodeCount > MAX_EXPRESSION_NODES) {
      throw new Error('Expression has too many operations');
    }
    return node;
  }
}

class BillingRuntime {
  constructor(variables) {
    this.variables = variables;
    this.matchedTier = '';
  }

  run(tree) {
    const value = requireNumber(this.evaluate(tree));
    if (!Number.isFinite(value)) {
      throw new Error('Expression result is not finite');
    }
    return { value, matchedTier: this.matchedTier };
  }

  evaluate(node) {
    switch (node.kind) {
      case 'literal':
        return node.value;
      case 'variable':
        return this.evaluateVariable(node.name);
      case 'unary': {
        const value = this.evaluate(node.operand);
        if (node.operator === '!') return !requireBoolean(value);
        const number = requireNumber(value);
        return node.operator === '-' ? -number : number;
      }
      case 'binary':
        return this.evaluateBinary(node);
      case 'conditional':
        return requireBoolean(this.evaluate(node.condition))
          ? this.evaluate(node.whenTrue)
          : this.evaluate(node.whenFalse);
      case 'call':
        return this.evaluateCall(node);
      default:
        throw new Error('Unsupported expression node');
    }
  }

  evaluateVariable(name) {
    if (!Object.hasOwn(this.variables, name)) {
      throw new Error(`Unknown variable: ${name}`);
    }
    const value = this.variables[name];
    if (!Number.isFinite(value)) {
      throw new Error(`Variable is not finite: ${name}`);
    }
    return value;
  }

  evaluateBinary(node) {
    if (node.operator === '&&') {
      const left = requireBoolean(this.evaluate(node.left));
      return left ? requireBoolean(this.evaluate(node.right)) : false;
    }
    if (node.operator === '||') {
      const left = requireBoolean(this.evaluate(node.left));
      return left ? true : requireBoolean(this.evaluate(node.right));
    }
    const left = this.evaluate(node.left);
    const right = this.evaluate(node.right);
    if (node.operator === '==') return left === right;
    if (node.operator === '!=') return left !== right;
    const leftNumber = requireNumber(left);
    const rightNumber = requireNumber(right);
    switch (node.operator) {
      case '+':
        return leftNumber + rightNumber;
      case '-':
        return leftNumber - rightNumber;
      case '*':
        return leftNumber * rightNumber;
      case '/':
        if (rightNumber === 0) throw new Error('Division by zero');
        return leftNumber / rightNumber;
      case '<':
        return leftNumber < rightNumber;
      case '<=':
        return leftNumber <= rightNumber;
      case '>':
        return leftNumber > rightNumber;
      case '>=':
        return leftNumber >= rightNumber;
      default:
        throw new Error(`Unsupported operator: ${node.operator}`);
    }
  }

  evaluateCall(node) {
    if (node.name === 'tier') {
      if (node.arguments.length !== 2) {
        throw new Error('tier expects two arguments');
      }
      const name = this.evaluate(node.arguments[0]);
      if (typeof name !== 'string') {
        throw new Error('tier name must be a string');
      }
      const value = requireNumber(this.evaluate(node.arguments[1]));
      this.matchedTier = name;
      return value;
    }
    const values = node.arguments.map((argument) =>
      requireNumber(this.evaluate(argument)),
    );
    switch (node.name) {
      case 'max':
        requireArgumentCount(node.name, values, 2);
        return Math.max(values[0], values[1]);
      case 'min':
        requireArgumentCount(node.name, values, 2);
        return Math.min(values[0], values[1]);
      case 'abs':
        requireArgumentCount(node.name, values, 1);
        return Math.abs(values[0]);
      case 'ceil':
        requireArgumentCount(node.name, values, 1);
        return Math.ceil(values[0]);
      case 'floor':
        requireArgumentCount(node.name, values, 1);
        return Math.floor(values[0]);
      default:
        throw new Error(`Unknown function: ${node.name}`);
    }
  }
}

export function evaluateSafeBillingExpression(expression, variables) {
  if (new TextEncoder().encode(expression).byteLength > MAX_EXPRESSION_BYTES) {
    throw new Error('Expression is too long');
  }
  const normalizedExpression = normalizeVersion(expression);
  const tree = new BillingParser(
    new BillingLexer(normalizedExpression),
  ).parse();
  if (validateBillingTree(tree) !== 'number') {
    throw new Error('Expression result must be a number');
  }
  return new BillingRuntime(variables).run(tree);
}

function normalizeVersion(expression) {
  const version = expression.match(/^v(\d+):(.*)$/s);
  if (!version) return expression;
  if (version[1] !== '1') {
    throw new Error(`Unsupported expression version: v${version[1]}`);
  }
  return version[2];
}

function validateBillingTree(node) {
  switch (node.kind) {
    case 'literal':
      return typeof node.value;
    case 'variable':
      if (!BILLING_VARIABLES.has(node.name)) {
        throw new Error(`Unknown variable: ${node.name}`);
      }
      return 'number';
    case 'unary': {
      const operandType = validateBillingTree(node.operand);
      const expectedType = node.operator === '!' ? 'boolean' : 'number';
      requireStaticType(operandType, expectedType);
      return expectedType;
    }
    case 'binary':
      return validateBinaryNode(node);
    case 'conditional': {
      requireStaticType(validateBillingTree(node.condition), 'boolean');
      const whenTrueType = validateBillingTree(node.whenTrue);
      const whenFalseType = validateBillingTree(node.whenFalse);
      if (whenTrueType !== whenFalseType) {
        throw new Error('Conditional branches must have the same type');
      }
      return whenTrueType;
    }
    case 'call':
      return validateCallNode(node);
    default:
      throw new Error('Unsupported expression node');
  }
}

function validateBinaryNode(node) {
  const leftType = validateBillingTree(node.left);
  const rightType = validateBillingTree(node.right);
  if (node.operator === '&&' || node.operator === '||') {
    requireStaticType(leftType, 'boolean');
    requireStaticType(rightType, 'boolean');
    return 'boolean';
  }
  if (node.operator === '==' || node.operator === '!=') {
    if (leftType !== rightType) {
      throw new Error('Equality operands must have the same type');
    }
    return 'boolean';
  }
  requireStaticType(leftType, 'number');
  requireStaticType(rightType, 'number');
  if (['<', '<=', '>', '>='].includes(node.operator)) return 'boolean';
  return 'number';
}

function validateCallNode(node) {
  if (node.name === 'tier') {
    requireNodeArgumentCount(node, 2);
    requireStaticType(validateBillingTree(node.arguments[0]), 'string');
    requireStaticType(validateBillingTree(node.arguments[1]), 'number');
    return 'number';
  }

  const argumentCounts = { max: 2, min: 2, abs: 1, ceil: 1, floor: 1 };
  if (!Object.hasOwn(argumentCounts, node.name)) {
    throw new Error(`Unknown function: ${node.name}`);
  }
  requireNodeArgumentCount(node, argumentCounts[node.name]);
  for (const argument of node.arguments) {
    requireStaticType(validateBillingTree(argument), 'number');
  }
  return 'number';
}

function requireNodeArgumentCount(node, expected) {
  if (node.arguments.length !== expected) {
    throw new Error(`${node.name} expects ${expected} arguments`);
  }
}

function requireStaticType(actual, expected) {
  if (actual !== expected) {
    throw new Error(`Expected a ${expected} expression`);
  }
}

function requireNumber(value) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error('Expected a finite number');
  }
  return value;
}

function requireBoolean(value) {
  if (typeof value !== 'boolean') {
    throw new Error('Expected a boolean condition');
  }
  return value;
}

function requireArgumentCount(name, values, expected) {
  if (values.length !== expected) {
    throw new Error(`${name} expects ${expected} arguments`);
  }
}
