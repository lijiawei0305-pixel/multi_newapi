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
import { SectionPageLayout } from '@/components/layout'
import { AgentsDialogs } from './components/agents-dialogs'
import { AgentsPrimaryButtons } from './components/agents-primary-buttons'
import { AgentsProvider } from './components/agents-provider'
import { AgentsTable } from './components/agents-table'

function AgentsContent() {
  const { t } = useTranslation()

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          {t('Sub-Agent Management', { defaultValue: '子代理管理' })}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <AgentsPrimaryButtons />
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div
            className='flex h-full min-h-0 flex-col gap-4'
            data-testid='agents-page'
          >
            <div className='min-h-0 flex-1'>
              <AgentsTable />
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <AgentsDialogs />
    </>
  )
}

export function Agents() {
  return (
    <AgentsProvider>
      <AgentsContent />
    </AgentsProvider>
  )
}
