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
import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ImageIcon, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getApiErrorCode } from '@/lib/api'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { getSiteConfig, updateSiteConfig, uploadLogo } from './api'

// Preset palette — must mirror the backend siteconfig defaultPalette (controlled).
const PALETTE = [
  '#1677ff',
  '#2f54eb',
  '#722ed1',
  '#13c2c2',
  '#52c41a',
  '#fadb14',
  '#fa8c16',
  '#fa541c',
  '#f5222d',
  '#eb2f96',
]

export function SiteBranding() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const fileRef = useRef<HTMLInputElement>(null)

  const [siteName, setSiteName] = useState('')
  const [brandHidden, setBrandHidden] = useState(false)
  const [themeColor, setThemeColor] = useState('#1677ff')
  const [logoUrl, setLogoUrl] = useState('')
  const [saving, setSaving] = useState(false)
  const [uploading, setUploading] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['tenant-site-config'],
    queryFn: async () => {
      const res = await getSiteConfig()
      return res.data
    },
  })

  // Initialize the form from server state (and after save/upload refetches).
  useEffect(() => {
    if (!data) return
    setSiteName(data.site_name || '')
    setBrandHidden(!!data.brand_hidden)
    setThemeColor(data.theme_color || '#1677ff')
    setLogoUrl(data.logo_url || '')
  }, [data])

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['tenant-site-config'] })

  const handleSave = async () => {
    setSaving(true)
    try {
      const res = await updateSiteConfig({
        site_name: siteName.trim(),
        brand_hidden: brandHidden,
        theme_color: themeColor,
      })
      if (res.success) {
        toast.success(t('Branding saved'))
        refresh()
      }
    } catch (err) {
      const code = getApiErrorCode(err)
      toast.error(
        code === 'THEME_NOT_IN_PALETTE'
          ? t('Selected theme color is not allowed')
          : t('Request failed')
      )
    } finally {
      setSaving(false)
    }
  }

  const handleLogoChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    e.target.value = '' // allow re-selecting the same file
    if (!file) return
    setUploading(true)
    try {
      const res = await uploadLogo(file)
      if (res.success && res.data?.logo_url) {
        setLogoUrl(res.data.logo_url)
        toast.success(t('Logo uploaded'))
        refresh()
      }
    } catch (err) {
      const code = getApiErrorCode(err)
      const map: Record<string, string> = {
        ASSET_TYPE_FORBIDDEN: t('Image type not allowed (jpg / png / webp only)'),
        ASSET_TOO_LARGE: t('Image too large (max 2MB)'),
      }
      toast.error((code && map[code]) || t('Upload failed'))
    } finally {
      setUploading(false)
    }
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Site Branding')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-2xl flex-col gap-4'>
          {isLoading && !data ? (
            <div className='flex justify-center py-10'>
              <Spinner />
            </div>
          ) : (
            <>
              <Card>
                <CardHeader>
                  <CardTitle>{t('Brand identity')}</CardTitle>
                  <CardDescription>
                    {t(
                      'Customize how your site appears to your users. On a custom domain with brand hiding on, the main-site name and logo are replaced by yours.'
                    )}
                  </CardDescription>
                </CardHeader>
                <CardContent className='flex flex-col gap-5'>
                  {/* Site name */}
                  <div className='flex flex-col gap-2'>
                    <Label htmlFor='site-name'>{t('Site Name')}</Label>
                    <Input
                      id='site-name'
                      value={siteName}
                      onChange={(e) => setSiteName(e.target.value)}
                      placeholder={t('Your site name')}
                    />
                  </div>

                  {/* Logo */}
                  <div className='flex flex-col gap-2'>
                    <Label>{t('Logo')}</Label>
                    <div className='flex items-center gap-3'>
                      <div className='bg-muted flex h-16 w-16 items-center justify-center overflow-hidden rounded-md border'>
                        {logoUrl ? (
                          <img
                            src={logoUrl}
                            alt='logo'
                            className='h-full w-full object-contain'
                          />
                        ) : (
                          <ImageIcon className='text-muted-foreground h-6 w-6' />
                        )}
                      </div>
                      <div className='flex flex-col gap-1'>
                        <Button
                          variant='outline'
                          size='sm'
                          disabled={uploading}
                          onClick={() => fileRef.current?.click()}
                        >
                          <Upload className='h-4 w-4' />
                          {uploading ? t('Uploading...') : t('Upload logo')}
                        </Button>
                        <span className='text-muted-foreground text-xs'>
                          {t('jpg / png / webp, up to 2MB')}
                        </span>
                      </div>
                      <input
                        ref={fileRef}
                        type='file'
                        accept='image/jpeg,image/png,image/webp'
                        className='hidden'
                        onChange={handleLogoChange}
                      />
                    </div>
                  </div>

                  {/* Theme color */}
                  <div className='flex flex-col gap-2'>
                    <Label>{t('Theme Color')}</Label>
                    <div className='flex flex-wrap gap-2'>
                      {PALETTE.map((c) => (
                        <button
                          key={c}
                          type='button'
                          onClick={() => setThemeColor(c)}
                          title={c}
                          className={
                            'flex h-8 w-8 items-center justify-center rounded-full border transition ' +
                            (themeColor === c
                              ? 'ring-primary ring-2 ring-offset-2'
                              : 'hover:scale-110')
                          }
                          style={{ backgroundColor: c }}
                        >
                          {themeColor === c && (
                            <Check className='h-4 w-4 text-white' />
                          )}
                        </button>
                      ))}
                    </div>
                  </div>

                  {/* Brand hidden */}
                  <div className='flex items-center justify-between gap-4 rounded-md border p-3'>
                    <div className='flex flex-col'>
                      <Label htmlFor='brand-hidden'>
                        {t('Hide main-site branding')}
                      </Label>
                      <span className='text-muted-foreground text-xs'>
                        {t(
                          'When on, your custom domain shows only your name and logo.'
                        )}
                      </span>
                    </div>
                    <Switch
                      id='brand-hidden'
                      checked={brandHidden}
                      onCheckedChange={setBrandHidden}
                    />
                  </div>

                  <div className='flex justify-end'>
                    <Button onClick={handleSave} disabled={saving}>
                      {saving ? t('Saving...') : t('Save')}
                    </Button>
                  </div>
                </CardContent>
              </Card>
            </>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
