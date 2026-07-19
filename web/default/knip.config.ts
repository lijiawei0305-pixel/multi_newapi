import type { KnipConfig } from 'knip'

const config: KnipConfig = {
  entry: [
    'src/main.tsx',
    'src/**/*.test.{ts,tsx}',
    'scripts/*.mjs',
    'scripts/**/*.test.mjs',
    // These directories are intentionally maintained as reusable component
    // libraries. Treat each module as a public entry so Knip follows its real
    // dependencies without requiring every exported primitive in the app.
    'src/components/ui/**/*.{ts,tsx}',
    'src/components/ai-elements/**/*.{ts,tsx}',
    'src/components/auto-skeleton.tsx',
  ],
  project: ['src/**/*.{ts,tsx}', 'scripts/**/*.mjs'],
  ignore: ['src/routeTree.gen.ts'],
  ignoreDependencies: [
    // Referenced from CSS rather than an analyzable JS/TS import.
    '@fontsource-variable/lora',
    '@fontsource-variable/public-sans',
    'tailwindcss',
    'tw-animate-css',
    // Invoked by the protected-header formatting wrapper via spawnSync.
    'oxfmt',
    // Retained as the project-aware component registry/scaffolding CLI.
    'shadcn',
  ],
}

export default config
