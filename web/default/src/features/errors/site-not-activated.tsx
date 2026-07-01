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
import { useTranslation } from 'react-i18next'

/**
 * Full-screen "站点未开通" gate. Rendered by the root when the current Host is an
 * unregistered `*.wedreamhub.com` subdomain (backend `SITE_NOT_ACTIVATED`): it is
 * neither the main site (www / apex) nor a provisioned agent site. Deliberately
 * neutral — it shows NO tenant data and NO main-site console, so an arbitrary
 * subdomain pointed at the platform IP never surfaces the main site.
 */
export function SiteNotActivated() {
  const { t } = useTranslation()
  return (
    <div className='h-svh'>
      <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2 px-6'>
        <h1 className='text-[5rem] leading-tight font-bold'>🚧</h1>
        <span className='text-xl font-medium'>{t('Site not activated')}</span>
        <p className='text-muted-foreground max-w-md text-center'>
          {t('This site is not open yet or the domain has not been bound.')}
        </p>
      </div>
    </div>
  )
}
