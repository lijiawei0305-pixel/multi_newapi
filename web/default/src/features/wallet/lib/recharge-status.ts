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

/**
 * 充值状态接口字段解释（与 GET /api/tenant/wallet/recharge/status 对齐）。
 * 纯函数，供轮询逻辑与单测共用，锁定 PAY-STA-01：仅 credited 视为到账完成。
 */

export type RechargeStatusFields = {
  status?: string
  provider_paid?: boolean
  credited?: boolean
  /** 兼容字段：服务端定义为仅 credited 时 true */
  paid?: boolean
  expires_at?: string
}

export type RechargeStatusInterpretation =
  | 'pending'
  | 'paid_processing'
  | 'credited'
  | 'failed'
  | 'expired'

/**
 * 将状态接口响应解释为前端 phase 语义。
 * - credited / paid(兼容)=true → credited（停止轮询、更新余额）
 * - provider_paid / status=paid → paid_processing（继续轮询）
 * - failed → failed
 * - expires_at 已过且未 provider_paid → expired
 */
export function interpretRechargeStatus(
  data: RechargeStatusFields,
  nowMs: number = Date.now()
): RechargeStatusInterpretation {
  const status = data.status ?? ''
  const isCredited =
    data.credited === true || status === 'credited' || data.paid === true
  if (isCredited) {
    return 'credited'
  }
  if (status === 'failed') {
    return 'failed'
  }
  const isProviderPaid =
    data.provider_paid === true || status === 'paid' || status === 'credited'
  if (
    data.expires_at &&
    Date.parse(data.expires_at) < nowMs &&
    !isProviderPaid
  ) {
    return 'expired'
  }
  if (isProviderPaid) {
    return 'paid_processing'
  }
  return 'pending'
}
