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
import { CircleCheck, ImageIcon, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getApiErrorCode } from '@/lib/api'
import {
  THEME_PRESETS,
  type ThemePreset,
} from '@/lib/theme-customization'
import { cn } from '@/lib/utils'
import { useThemeCustomization } from '@/context/theme-customization-provider'
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

export function SiteBranding() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const fileRef = useRef<HTMLInputElement>(null)
  // Reuse the app's theme system so picking a style previews live for the agent.
  const { setPreset } = useThemeCustomization()

  const [siteName, setSiteName] = useState('')
  const [footer, setFooter] = useState('')
  const [brandHidden, setBrandHidden] = useState(false)
  const [themePreset, setThemePreset] = useState<ThemePreset>('default')
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
    setFooter(data.footer || '')
    setBrandHidden(!!data.brand_hidden)
    setThemePreset((data.theme_preset as ThemePreset) || 'default')
    setLogoUrl(data.logo_url || '')
  }, [data])

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['tenant-site-config'] })

  // Pick a style: preview live for the agent, and stage it for save.
  const pickPreset = (value: ThemePreset) => {
    setThemePreset(value)
    setPreset(value)
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      const res = await updateSiteConfig({
        site_name: siteName.trim(),
        footer: footer.trim(),
        brand_hidden: brandHidden,
        theme_preset: themePreset,
      })
      if (res.success) {
        toast.success(t('Branding saved'))
        refresh()
      }
    } catch (err) {
      const code = getApiErrorCode(err)
      toast.error(
        code === 'THEME_PRESET_INVALID'
          ? t('Selected style is not allowed')
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

                {/* Footer —— 留空则前端自动用「© 年份 站名」兜底（见 use-tenant-brand.ts）；
                    绝不能让它落到 <Footer /> 的默认分支，那会显示 New API 的文档链接大列。 */}
                <div className='flex flex-col gap-2'>
                  <Label htmlFor='site-footer'>{t('Footer')}</Label>
                  <Input
                    id='site-footer'
                    value={footer}
                    onChange={(e) => setFooter(e.target.value)}
                    placeholder={t('e.g. © 2026 Your Brand. All rights reserved.')}
                  />
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Shown at the bottom of your site. Supports HTML. Leave empty to use "© year + your site name".'
                    )}
                  </p>
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

                {/* Default style (theme preset) */}
                <div className='flex flex-col gap-2'>
                  <Label>{t('Default style')}</Label>
                  <span className='text-muted-foreground text-xs'>
                    {t(
                      'The default visual style your users see. They can still change it themselves from the top-right theme menu.'
                    )}
                  </span>
                  <div className='mt-1 grid grid-cols-4 gap-3 sm:grid-cols-5'>
                    {THEME_PRESETS.map((preset) => {
                      const selected = themePreset === preset.value
                      return (
                        <button
                          key={preset.value}
                          type='button'
                          onClick={() => pickPreset(preset.value)}
                          className='group flex flex-col items-stretch outline-none'
                          aria-label={t(`preset.${preset.value}`)}
                        >
                          <div
                            className={cn(
                              'ring-border relative h-12 rounded-md ring-[1px] transition',
                              selected
                                ? 'ring-primary shadow-md'
                                : 'group-hover:ring-primary/60'
                            )}
                          >
                            <div
                              aria-hidden='true'
                              className='absolute inset-0 rounded-md'
                              style={
                                preset.value === 'default'
                                  ? {
                                      background:
                                        'linear-gradient(135deg, var(--background) 0%, var(--muted) 50%, var(--foreground) 100%)',
                                    }
                                  : {
                                      background: `linear-gradient(135deg, ${preset.swatches[0]} 0%, ${preset.swatches[1] ?? preset.swatches[0]} 100%)`,
                                    }
                              }
                            />
                            {selected && (
                              <CircleCheck
                                className='fill-primary absolute top-0 right-0 z-10 size-5 translate-x-1/2 -translate-y-1/2 stroke-white'
                                aria-hidden='true'
                              />
                            )}
                          </div>
                          <div className='mt-1.5 truncate text-center text-xs'>
                            {t(`preset.${preset.value}`)}
                          </div>
                        </button>
                      )
                    })}
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
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
