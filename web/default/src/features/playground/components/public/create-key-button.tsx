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
import { PlusIcon } from 'lucide-react'
import { useNavigate } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth-store'
import { cn } from '@/lib/utils'

interface CreateKeyButtonProps {
  className?: string
}

export function CreateKeyButton({ className }: CreateKeyButtonProps) {
  const navigate = useNavigate()
  const { auth } = useAuthStore()
  const isAuthed = !!auth.user

  function handleClick() {
    if (isAuthed) {
      void navigate({ to: '/keys' })
    } else {
      void navigate({ to: '/sign-in' })
    }
  }

  return (
    <Button
      variant="link"
      size="default"
      className={cn('gap-1', className)}
      onClick={handleClick}
    >
      <PlusIcon />
      创建 API 密钥
    </Button>
  )
}
