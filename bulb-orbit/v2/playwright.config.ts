import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: 'tests',
  webServer: { command: 'npm run build && npm run preview', url: 'http://localhost:4173', reuseExistingServer: false },
  use: { baseURL: 'http://localhost:4173', viewport: { width: 1920, height: 1080 } },
})
