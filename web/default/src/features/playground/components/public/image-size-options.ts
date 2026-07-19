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

export interface ImageSizeOption {
  value: string
  ratio: string
  orientation: string
  w: number
  h: number
}

export const IMAGE_SIZE_OPTIONS: ImageSizeOption[] = [
  { value: '1024x1024', ratio: '1:1', orientation: 'Square', w: 1024, h: 1024 },
  {
    value: '1536x1024',
    ratio: '3:2',
    orientation: 'Landscape',
    w: 1536,
    h: 1024,
  },
  {
    value: '1024x1536',
    ratio: '2:3',
    orientation: 'Portrait',
    w: 1024,
    h: 1536,
  },
]

export const DEFAULT_IMAGE_SIZE = '1024x1024'
