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
import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, Gift } from 'lucide-react'
import { toast } from 'sonner'
import { agentContextQueryOptions } from '@/lib/agent-context'
import { computeTimeRange } from '@/lib/time'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  getMyWithdrawals,
  getPayoutAccount,
  getTenantEarnings,
} from '@/features/agent-earnings/api'
import { MyWithdrawalsTable } from '@/features/agent-earnings/components/my-withdrawals-table'
import { PayoutAccountCard } from '@/features/agent-earnings/components/payout-account-card'
import { PayoutAccountDialog } from '@/features/agent-earnings/components/payout-account-dialog'
import { WithdrawDialog } from '@/features/agent-earnings/components/withdraw-dialog'
import { parseEarnings } from '@/features/agent-earnings/lib'
import { getTenantFinanceSummary } from '@/features/financial-report/api'
import type { RangeParams } from '@/features/financial-report/types'
import {
  createPromotionChannel,
  getPromotionChannels,
} from '@/features/promotion-channels/api'

// ============================================================================
// 「代理邀请返现」面板(doc/l0-agent-wallet-referral.md):仅普通代理(L0,
// is_agent_owner && level===0)在主站钱包页可见。L0 没独立站,靠这块分享邀请
// 链接、看邀请人数/被邀请消费·套餐/返现,并绑收款账户提现——机制同 L1,数据全走
// 现成的 L0 可调 /api/tenant/* 端点,返现(consume_commission + tokenplan_spread)
// 早已自动入代理钱包。收款/提现组件复用 agent-earnings。
// ============================================================================

const cny = (v: number) =>
  `¥${(Number(v) || 0).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`

function Stat({
  label,
  value,
  emphasis,
}: {
  label: string
  value: string
  emphasis?: boolean
}) {
  return (
    <div className='rounded-lg border px-3 py-2'>
      <div className='text-muted-foreground text-xs'>{label}</div>
      <div
        className={`mt-1 font-mono text-base font-bold tabular-nums ${emphasis ? 'text-emerald-600' : ''}`}
      >
        {value}
      </div>
    </div>
  )
}

export function AgentReferralRewardsCard() {
  const qc = useQueryClient()
  const [withdrawOpen, setWithdrawOpen] = useState(false)
  const [payoutOpen, setPayoutOpen] = useState(false)

  const { data: ctx } = useQuery(agentContextQueryOptions)
  const isL0Agent = !!ctx?.is_agent_owner && ctx?.level === 0

  // 邀请渠道(取邀请链接 + 邀请总人数)。
  const channelsQuery = useQuery({
    queryKey: ['tenant-promotion-channels'],
    queryFn: async () => (await getPromotionChannels()).data ?? [],
    enabled: isL0Agent,
    placeholderData: (p) => p,
  })
  const channels = channelsQuery.data ?? []
  const primary = channels[0]
  const inviteLink = primary
    ? `${window.location.origin}/sign-up?channel=${primary.code}`
    : ''
  const invitedCount = channels.reduce(
    (s, c) => s + (c.registered_count || 0),
    0
  )

  // 被邀请消费/套餐 + 返现(finance summary 概览)；累计口径取很宽的区间。
  const [range] = useState<RangeParams>(() => computeTimeRange(3650))
  const financeQuery = useQuery({
    queryKey: ['tenant-finance-summary', range],
    queryFn: () => getTenantFinanceSummary(range),
    select: (res) => res.data,
    enabled: isL0Agent,
    placeholderData: (p) => p,
  })
  const overview = financeQuery.data?.overview
  const apiConsumption = overview?.apikey_consumption_cny ?? 0
  const planPurchase = overview?.tokenplan_revenue_cny ?? 0
  const totalRebate =
    (overview?.consumption_withdrawable_cny ?? 0) +
    (overview?.tokenplan_withdrawable_cny ?? 0)

  // 可提现余额(代理钱包,消耗返现+套餐返现都入这里)。
  const { data: earningsRes } = useQuery({
    queryKey: ['tenant-earnings'],
    queryFn: getTenantEarnings,
    enabled: isL0Agent,
    placeholderData: (p) => p,
  })
  const { summary } = useMemo(
    () => parseEarnings(earningsRes?.data),
    [earningsRes]
  )
  const withdrawable = summary.withdrawable_cny

  const { data: payoutRes, isLoading: payoutLoading } = useQuery({
    queryKey: ['tenant-payout-account'],
    queryFn: getPayoutAccount,
    enabled: isL0Agent,
    placeholderData: (p) => p,
  })
  const payoutAccount = payoutRes?.data

  const { data: withdrawals, isLoading: wdLoading } = useQuery({
    queryKey: ['tenant-withdrawals'],
    queryFn: async () => (await getMyWithdrawals()).data ?? [],
    enabled: isL0Agent,
    placeholderData: (p) => p,
  })

  const createMut = useMutation({
    mutationFn: () => createPromotionChannel('默认邀请'),
    onSuccess: () => {
      toast.success('邀请链接已生成')
      qc.invalidateQueries({ queryKey: ['tenant-promotion-channels'] })
    },
    onError: () => toast.error('生成失败'),
  })

  const copyLink = () => {
    if (!inviteLink) return
    navigator.clipboard?.writeText(inviteLink)
    toast.success('已复制邀请链接')
  }

  const handleOpenWithdraw = () => {
    if (payoutAccount && !payoutAccount.configured) {
      toast.error('请先设置收款账户，再申请提现')
      setPayoutOpen(true)
      return
    }
    setWithdrawOpen(true)
  }

  if (!isL0Agent) return null

  return (
    <>
      <div className='rounded-lg border p-4 sm:p-5' data-testid='agent-referral-card'>
        <div className='mb-3 flex flex-wrap items-center gap-2'>
          <Gift className='text-primary size-4' />
          <h3 className='text-sm font-semibold'>代理邀请返现</h3>
          <span className='text-muted-foreground text-xs'>
            分享邀请链接，按下级的 API 消费和套餐购买给你返现
          </span>
        </div>

        {/* 邀请链接 */}
        <div className='mb-4'>
          {inviteLink ? (
            <div className='flex items-center gap-2'>
              <Input
                readOnly
                value={inviteLink}
                className='font-mono text-xs'
                onFocus={(e) => e.currentTarget.select()}
              />
              <Button size='sm' variant='outline' onClick={copyLink}>
                <Copy className='size-4' /> 复制
              </Button>
            </div>
          ) : (
            <Button
              size='sm'
              onClick={() => createMut.mutate()}
              disabled={createMut.isPending || channelsQuery.isLoading}
            >
              {createMut.isPending ? '生成中…' : '生成邀请链接'}
            </Button>
          )}
        </div>

        {/* 统计 */}
        <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5'>
          <Stat label='邀请总人数' value={String(invitedCount)} />
          <Stat label='被邀请 API 消费' value={cny(apiConsumption)} />
          <Stat label='被邀请套餐购买' value={cny(planPurchase)} />
          <Stat label='累计返现' value={cny(totalRebate)} />
          <Stat label='可提现' value={cny(withdrawable)} emphasis />
        </div>

        {/* 收款账户 + 提现 */}
        <div className='mt-4 flex flex-col gap-3'>
          <div className='flex items-center justify-between gap-2'>
            <span className='text-sm font-medium'>收款与提现</span>
            <Button
              size='sm'
              onClick={handleOpenWithdraw}
              disabled={!(withdrawable > 0)}
              data-testid='referral-withdraw-btn'
            >
              申请提现
            </Button>
          </div>
          <PayoutAccountCard
            account={payoutAccount}
            loading={payoutLoading}
            onEdit={() => setPayoutOpen(true)}
          />
          <div className='overflow-hidden rounded-lg border'>
            <MyWithdrawalsTable items={withdrawals || []} loading={wdLoading} />
          </div>
        </div>
      </div>

      <WithdrawDialog
        open={withdrawOpen}
        onOpenChange={setWithdrawOpen}
        max={withdrawable}
        onSuccess={() => {
          qc.invalidateQueries({ queryKey: ['tenant-earnings'] })
          qc.invalidateQueries({ queryKey: ['tenant-withdrawals'] })
        }}
        onPayoutAccountRequired={() => setPayoutOpen(true)}
      />
      <PayoutAccountDialog
        open={payoutOpen}
        onOpenChange={setPayoutOpen}
        account={payoutAccount}
        onSaved={() =>
          qc.invalidateQueries({ queryKey: ['tenant-payout-account'] })
        }
      />
    </>
  )
}
