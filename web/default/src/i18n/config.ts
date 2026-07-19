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
import i18n, { type BackendModule, type ReadCallback } from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

type LocaleMessages = Record<string, string>
type LocaleModule = { default: { translation: LocaleMessages } }

// Keep locale catalogs out of the bootstrap bundle. The detector chooses a
// language first, then i18next fetches only that catalog (and the English
// fallback if a key is genuinely absent) as an async chunk.
const localeLoaders: Record<string, () => Promise<LocaleModule>> = {
  en: () => import('./locales/en.json'),
  fr: () => import('./locales/fr.json'),
  ja: () => import('./locales/ja.json'),
  ru: () => import('./locales/ru.json'),
  vi: () => import('./locales/vi.json'),
  zh: () => import('./locales/zh.json'),
}

const localeBackend: BackendModule = {
  type: 'backend',
  init: () => undefined,
  async read(language: string, _namespace: string, callback: ReadCallback) {
    const loader = localeLoaders[language] ?? localeLoaders.en
    try {
      const module = await loader()
      callback(null, module.default.translation)
    } catch (error: unknown) {
      callback(error as Error, false)
    }
  },
}

i18n
  .use(localeBackend)
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    fallbackLng: 'en',
    supportedLngs: ['en', 'zh', 'fr', 'ru', 'ja', 'vi'],
    load: 'languageOnly', // Convert zh-CN -> zh
    nsSeparator: false, // Allow literal colons in keys (e.g., URLs, labels)
    debug: import.meta.env.DEV,
    interpolation: {
      escapeValue: false, // not needed for react as it escapes by default
    },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
    },
  })

export default i18n
