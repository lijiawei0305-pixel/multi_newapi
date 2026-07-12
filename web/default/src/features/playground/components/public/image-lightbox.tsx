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
import { DownloadIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'

export interface ImageLightboxProps {
  /** 当前放大的图片地址；为空表示无选中（配合 open=false 关闭） */
  src: string | null
  alt?: string
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 点击“下载”时回调（沿用工作区统一的鉴权下载逻辑） */
  onDownload?: () => void
}

// 点击图片放大预览：受控 Dialog + 透明大画布，Esc / 点遮罩 / 右上角 × 均可关闭。
export function ImageLightbox({
  src,
  alt,
  open,
  onOpenChange,
  onDownload,
}: ImageLightboxProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-w-[92vw] border-0 bg-transparent p-0 shadow-none ring-0 sm:max-w-[86vw]'>
        <DialogTitle className='sr-only'>图片预览</DialogTitle>
        {src && (
          <div className='flex flex-col items-center gap-3'>
            <img
              src={src}
              alt={alt || '预览图片'}
              className='max-h-[82vh] w-auto max-w-full rounded-lg object-contain shadow-2xl'
            />
            {onDownload && (
              <Button size='sm' variant='secondary' onClick={onDownload}>
                <DownloadIcon className='mr-1.5 size-4' />
                下载原图
              </Button>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
