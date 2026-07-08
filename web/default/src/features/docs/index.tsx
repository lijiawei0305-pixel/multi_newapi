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

import { CopyButton } from '@/components/copy-button'
import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { Card, CardContent } from '@/components/ui/card'
import { useStatus } from '@/hooks/use-status'

export function Docs() {
  const { t } = useTranslation()
  const { status } = useStatus()

  const brand =
    (status?.system_name as string | undefined) ||
    t('Docs Brand Fallback', { defaultValue: '本平台' })
  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const baseUrl = `${origin}/v1`

  const steps = [
    {
      no: '01',
      title: t('Docs Step1 Title', { defaultValue: '注册账号' }),
      desc: t('Docs Step1 Desc', {
        defaultValue: '在本站注册账号，或通过代理邀请链接 / 邀请码加入',
      }),
    },
    {
      no: '02',
      title: t('Docs Step2 Title', { defaultValue: '获取令牌' }),
      desc: t('Docs Step2 Desc', {
        defaultValue: '进入控制台 → API 密钥 → 新建，复制 sk-xxx',
      }),
    },
    {
      no: '03',
      title: t('Docs Step3 Title', { defaultValue: '替换地址' }),
      desc: t('Docs Step3 Desc', {
        defaultValue: '将 base_url 换成本站地址，请求头 Authorization: Bearer 你的令牌',
      }),
    },
    {
      no: '04',
      title: t('Docs Step4 Title', { defaultValue: '选择模型' }),
      desc: t('Docs Step4 Desc', {
        defaultValue: '在「模型广场」查看可用模型，把名称填入 model 参数',
      }),
    },
  ]

  const sampleCode = `from openai import OpenAI

client = OpenAI(
    api_key="sk-your-token",
    base_url="${baseUrl}",
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "${t('Docs Sample Message', { defaultValue: '你好' })}"}],
)
print(response.choices[0].message.content)`

  const h2 = 'text-foreground text-xl font-semibold md:text-2xl'

  return (
    <PublicLayout showMainContainer={false}>
      <main className='mx-auto w-full max-w-5xl px-4 pt-24 pb-16'>
        <header>
          <p className='text-muted-foreground text-xs font-medium tracking-widest uppercase'>
            {t('Docs Eyebrow', { defaultValue: '开发文档' })}
          </p>
          <h1 className='text-foreground mt-3 text-4xl font-bold md:text-5xl'>
            {t('Docs Title', { defaultValue: '使用文档' })}
          </h1>
          <p className='text-muted-foreground mt-3 text-base'>
            {brand} {t('Docs Subtitle', { defaultValue: 'API 接入指南' })}
          </p>
        </header>
        <hr className='border-border my-10' />

        <section>
          <h2 className={h2}>
            {t('Docs QuickStart', { defaultValue: '快速开始' })}
          </h2>
          <Card className='mt-6'>
            <CardContent className='p-6'>
              <p className='text-muted-foreground'>
                {t('Docs QuickStart Desc', {
                  defaultValue: '将您的 OpenAI SDK base_url 替换为以下地址即可接入：',
                })}
              </p>
              <div className='bg-muted mt-4 flex items-center justify-between gap-3 rounded-md px-4 py-3'>
                <code className='font-mono text-sm break-all'>{baseUrl}</code>
                <CopyButton value={baseUrl} />
              </div>
            </CardContent>
          </Card>
        </section>

        <section className='mt-16'>
          <h2 className={h2}>{t('Docs Steps', { defaultValue: '接入步骤' })}</h2>
          <div className='mt-6 flex flex-col gap-4'>
            {steps.map((s) => (
              <Card key={s.no}>
                <CardContent className='flex items-start gap-5 p-6'>
                  <span className='text-muted-foreground/40 text-2xl font-bold tabular-nums'>
                    {s.no}
                  </span>
                  <div>
                    <h3 className='text-foreground font-semibold'>{s.title}</h3>
                    <p className='text-muted-foreground mt-1 text-sm'>
                      {s.desc}
                    </p>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </section>

        <section className='mt-16'>
          <h2 className={h2}>{t('Docs Sample', { defaultValue: '示例代码' })}</h2>
          <Card className='mt-6'>
            <CardContent className='relative p-6'>
              <div className='absolute top-4 right-4'>
                <CopyButton value={sampleCode} />
              </div>
              <pre className='overflow-x-auto'>
                <code className='font-mono text-sm leading-relaxed'>
                  {sampleCode}
                </code>
              </pre>
            </CardContent>
          </Card>
        </section>

        <section className='mt-16'>
          <h2 className={h2}>
            {t('Docs Contact', { defaultValue: '联系支持' })}
          </h2>
          <Card className='mt-6'>
            <CardContent className='p-6'>
              <p className='text-muted-foreground'>
                {t('Docs Contact Desc', {
                  defaultValue: '如有问题，请通过以下方式联系我们：',
                })}
              </p>
              <div className='mt-4 flex items-center gap-2'>
                <span className='text-muted-foreground'>
                  {t('Docs Contact WeChat', { defaultValue: '微信：' })}
                </span>
                <code className='text-foreground font-mono'>
                  chen13477359255
                </code>
                <CopyButton value='chen13477359255' />
              </div>
            </CardContent>
          </Card>
        </section>
      </main>
      <Footer />
    </PublicLayout>
  )
}
