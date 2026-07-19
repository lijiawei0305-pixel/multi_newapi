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
import { Info } from 'lucide-react'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

interface GatingAlertProps {
  message: string
  /** 可选内联行动按钮（如「去登录」「创建 API 密钥」），缩短转化路径 */
  action?: { label: string; onClick: () => void }
}

export function GatingAlert({ message, action }: GatingAlertProps) {
  // 门控是「常态引导」而非「警告」，用中性 muted 信息态更克制专业（非 warning 黄）。
  return (
    <Alert className='border-border bg-muted/40 flex items-center gap-3'>
      <Info className='text-muted-foreground' />
      <AlertDescription className='text-foreground flex-1'>
        {message}
      </AlertDescription>
      {action && (
        <Button
          size='sm'
          variant='outline'
          className='shrink-0'
          onClick={action.onClick}
        >
          {action.label}
        </Button>
      )}
    </Alert>
  )
}
