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
import {
  Activity,
  Box,
  ChartColumnBig,
  Coins,
  CreditCard,
  FileText,
  FlaskConical,
  Gauge,
  Gift,
  Globe,
  HandCoins,
  Key,
  Layers,
  LayoutDashboard,
  LifeBuoy,
  ListTodo,
  Megaphone,
  MessageSquare,
  Network,
  Package,
  Palette,
  Percent,
  Radio,
  ScrollText,
  ServerCog,
  Settings,
  ShieldAlert,
  ShoppingBag,
  Tags,
  Ticket,
  User,
  Users,
  UsersRound,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ROLE } from '@/lib/roles'
import { type SidebarData } from '@/components/layout/types'

/**
 * Root navigation groups for the application sidebar.
 *
 * These are shown when the URL does not match any nested sidebar view
 * registered in `layout/lib/sidebar-view-registry.ts`.
 */
export function useSidebarData(): SidebarData {
  const { t } = useTranslation()

  return {
    navGroups: [
      {
        id: 'chat',
        title: t('Chat'),
        items: [
          {
            title: t('Playground'),
            url: '/playground',
            icon: FlaskConical,
          },
          {
            title: t('Chat'),
            icon: MessageSquare,
            type: 'chat-presets',
          },
        ],
      },
      {
        id: 'general',
        title: t('General'),
        items: [
          {
            title: t('Overview'),
            url: '/dashboard/overview',
            icon: Activity,
          },
          {
            title: t('Dashboard'),
            url: '/dashboard/models',
            icon: LayoutDashboard,
          },
          {
            title: t('API Keys'),
            url: '/keys',
            icon: Key,
          },
          {
            title: t('Usage Logs'),
            url: '/usage-logs/common',
            icon: FileText,
          },
          {
            title: t('Task Logs'),
            url: '/usage-logs/task',
            activeUrls: ['/usage-logs/drawing'],
            configUrls: ['/usage-logs/drawing', '/usage-logs/task'],
            icon: ListTodo,
          },
        ],
      },
      {
        id: 'personal',
        title: t('Personal'),
        items: [
          {
            title: t('Buy Plans'),
            url: '/plans',
            icon: ShoppingBag,
          },
          {
            title: t('Become Agent', { defaultValue: '开通代理' }),
            url: '/become-agent',
            icon: HandCoins,
          },
          {
            title: t('Wallet'),
            url: '/wallet',
            icon: Wallet,
          },
          {
            title: t('My Earnings'),
            url: '/agent-earnings',
            icon: Coins,
            agentOwnerOnly: true,
          },
          {
            title: t('Support Tickets'),
            url: '/tickets',
            icon: LifeBuoy,
          },
          {
            title: t('Profile'),
            url: '/profile',
            icon: User,
          },
        ],
      },
      {
        id: 'agent',
        title: t('Agent Self-Service'),
        agentOwnerOnly: true,
        items: [
          {
            title: t('Plan Listings'),
            url: '/agent-listings',
            icon: Tags,
          },
          {
            title: t('Promotion Channels'),
            url: '/promotion-channels',
            icon: Megaphone,
          },
          {
            title: t('Redemption Codes'),
            url: '/redemptions',
            icon: Gift,
          },
          {
            title: t('My Users'),
            url: '/my-users',
            icon: UsersRound,
          },
          {
            title: t('Model Group Multipliers'),
            url: '/my-groups',
            icon: Percent,
          },
          {
            title: t('My Banned Words'),
            url: '/my-moderation',
            icon: ShieldAlert,
          },
          {
            title: t('My Violation Logs'),
            url: '/my-violations',
            icon: ScrollText,
          },
          {
            title: t('Custom Domain'),
            url: '/custom-domain',
            icon: Globe,
            agentLevelMin: 1,
          },
          {
            title: t('Site Branding'),
            url: '/site-branding',
            icon: Palette,
            agentLevelMin: 1,
          },
          {
            title: t('Support Tickets'),
            url: '/agent-tickets',
            icon: LifeBuoy,
            agentOwnerOnly: true,
          },
        ],
      },
      {
        id: 'admin',
        title: t('Admin'),
        items: [
          {
            title: t('Channels'),
            url: '/channels',
            icon: Radio,
          },
          {
            title: t('Models'),
            url: '/models/metadata',
            icon: Box,
          },
          {
            title: t('Model Group Management'),
            url: '/model-groups',
            icon: Layers,
          },
          {
            title: t('Users'),
            url: '/users',
            icon: Users,
          },
          {
            title: t('Redemption Codes'),
            url: '/redemption-codes',
            icon: Ticket,
          },
          {
            title: t('Subscriptions'),
            url: '/subscriptions',
            icon: CreditCard,
          },
          {
            title: t('Token Plans'),
            url: '/token-plans',
            icon: Package,
          },
          {
            title: t('Agent Plans Admin', { defaultValue: '代理套餐' }),
            url: '/agent-plans',
            icon: Tags,
          },
          {
            title: t('Subscription Monitor'),
            url: '/subscription-monitor',
            icon: Gauge,
          },
          {
            title: t('Sub-Agent Management'),
            url: '/agents',
            icon: Network,
          },
          {
            title: t('Custom Domains'),
            url: '/admin-custom-domains',
            icon: Globe,
          },
          {
            title: t('Withdrawal Review'),
            url: '/withdrawals',
            icon: HandCoins,
          },
          {
            title: t('Banned Words Library'),
            url: '/moderation-words',
            icon: ShieldAlert,
          },
          {
            title: t('Violation Logs'),
            url: '/moderation-violations',
            icon: ScrollText,
          },
          {
            title: t('Payment Reconcile'),
            url: '/payment-reconcile',
            icon: HandCoins,
          },
          {
            title: t('Support Tickets'),
            url: '/admin-tickets',
            icon: LifeBuoy,
            requiredRole: ROLE.ADMIN,
          },
          {
            title: t('Financial Report'),
            url: '/admin/finance-report',
            icon: ChartColumnBig,
            requiredRole: ROLE.ADMIN,
          },
          {
            title: t('System Info'),
            url: '/system-info',
            icon: ServerCog,
            requiredRole: ROLE.SUPER_ADMIN,
          },
          {
            title: t('System Settings'),
            url: '/system-settings/site',
            activeUrls: ['/system-settings'],
            icon: Settings,
          },
        ],
      },
    ],
  }
}
