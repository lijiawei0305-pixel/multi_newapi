import tseslint from 'typescript-eslint'
export default tseslint.config(
  ...tseslint.configs.recommended,
  { ignores: ['dist', 'node_modules', 'public'] },
  // .cjs is always CommonJS regardless of package.json "type": "module" — require()
  // there is correct, not a lint violation.
  { files: ['**/*.cjs'], rules: { '@typescript-eslint/no-require-imports': 'off' } },
)
