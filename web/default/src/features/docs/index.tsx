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


  // ── 平台手册(合同三期交付 #13,内容随功能定稿持续更新)──────────────────────
  const manuals = [
    {
      title: t('Docs Manual Marketing Title', { defaultValue: '营销页与模板说明' }),
      intro: t('Docs Manual Marketing Intro', {
        defaultValue: '套餐购买页营销位与站点视觉模板的运营配置说明。',
      }),
      points: [
        t('Docs Manual Marketing P1', {
          defaultValue:
            '营销字段:后台「套餐管理」每档可配置 原价(划线价)、折扣角标、推荐标记,购买页自动渲染划线对比、角标与推荐高亮。',
        }),
        t('Docs Manual Marketing P2', {
          defaultValue:
            '价格与限额:售价为人民币结算价;月限额为套餐内可用额度(按美元计量),自购买日起按套餐有效期(如 30 天)生效。',
        }),
        t('Docs Manual Marketing P3', {
          defaultValue:
            '站点模板:独立(OEM)代理在「站点装修」中选择主题预设——内置 10 套视觉风格,保存后其站点即刻生效。',
        }),
        t('Docs Manual Marketing P4', {
          defaultValue:
            '上架与改价:代理在「套餐上架」自助上/下架,并可在保护线之上调整零售价;低于保护线的定价会被拒绝。',
        }),
      ],
    },
    {
      title: t('Docs Manual Domain Title', { defaultValue: 'OEM 域名接入与证书配置手册' }),
      intro: t('Docs Manual Domain Intro', {
        defaultValue: '独立代理绑定自有域名并自动启用 HTTPS 的完整流程。',
      }),
      points: [
        t('Docs Manual Domain P1', {
          defaultValue: '绑定:代理控制台「自定义域名」输入你的域名并提交。',
        }),
        t('Docs Manual Domain P2', {
          defaultValue:
            'DNS 配置:按页面指引到域名注册商添加 A 记录(指向平台服务器)与 TXT 验证记录;使用 Cloudflare 时验证期间请勿开启橙云代理。',
        }),
        t('Docs Manual Domain P3', {
          defaultValue:
            '验证与签发:回到页面点击「已配置 DNS——验证」,通过后系统自动签发 HTTPS 证书(通常数分钟内完成)。',
        }),
        t('Docs Manual Domain P4', {
          defaultValue:
            '生效规则:证书签发成功后域名转为 active 并进入访问解析;未完成签发的域名不会对外提供服务。',
        }),
        t('Docs Manual Domain P5', {
          defaultValue:
            '续期与到期提醒:证书由系统每日自动续期;到期前 30 天起,代理页与后台会出现到期提醒,若提醒持续存在请联系管理员。',
        }),
        t('Docs Manual Domain P6', {
          defaultValue:
            '管理员侧:后台「自定义域名」可跨租户查看域名状态与证书到期时间,必要时可强制解绑。',
        }),
      ],
    },
    {
      title: t('Docs Manual Renewal Title', { defaultValue: '续订说明(到期提醒与一键续费)' }),
      intro: t('Docs Manual Renewal Intro', {
        defaultValue: '套餐到期的提醒机制与续费操作路径。',
      }),
      points: [
        t('Docs Manual Renewal P1', {
          defaultValue:
            '到期提醒:套餐将到期(7 天内)或刚到期(7 天宽限内),控制台顶部会出现横幅提醒;用量达 80% / 100% 时同样有横幅。',
        }),
        t('Docs Manual Renewal P2', {
          defaultValue:
            '一键续费:点击横幅上的「立即续费」,系统直达购买页并自动对原套餐发起下单、弹出支付码,扫码即完成续费(新周期自购买日起算)。',
        }),
        t('Docs Manual Renewal P3', {
          defaultValue:
            '续费记录:「我的套餐」可查看历史订阅与状态;每笔订单在后台「支付对账」页留痕可查。',
        }),
        t('Docs Manual Renewal P4', {
          defaultValue:
            '失败与补救:支付失败会即时提示;已付款未生效的订单由系统每 5 分钟自动对账补激活,管理员也可在「支付对账」页手动触发。',
        }),
        t('Docs Manual Renewal P5', {
          defaultValue:
            '说明:自动免密扣款(签约代扣)暂未开通,续费需手动完成支付。',
        }),
      ],
    },
    {
      title: t('Docs Manual I18n Title', { defaultValue: '多语言与多币种配置说明' }),
      intro: t('Docs Manual I18n Intro', {
        defaultValue: '界面语言能力与币种口径说明。',
      }),
      points: [
        t('Docs Manual I18n P1', {
          defaultValue:
            '语言切换:页面右上角语言切换器支持 简体中文 / English / Français / 日本語 / Русский / Tiếng Việt 六种界面语言,偏好保存在浏览器本地。',
        }),
        t('Docs Manual I18n P2', {
          defaultValue:
            '覆盖范围:平台功能页文案已全量覆盖上述六语;个别第三方或上游内容以英文回退显示。',
        }),
        t('Docs Manual I18n P3', {
          defaultValue:
            '语言扩展:新增语言只需增加对应语言包文件并登记到语言列表,无需改动功能代码。',
        }),
        t('Docs Manual I18n P4', {
          defaultValue:
            '多币种:当前定价与结算均为人民币(¥);多币种展示(基于汇率换算)已在规划中,暂未开放。',
        }),
      ],
    },
  ]

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
            {t('Docs Manuals', { defaultValue: '平台手册' })}
          </h2>
          <p className='text-muted-foreground mt-3 text-sm'>
            {t('Docs Manuals Desc', {
              defaultValue:
                '面向站长与代理的运营手册:营销位配置、OEM 域名接入、套餐续费与多语言能力。',
            })}
          </p>
          <div className='mt-6 flex flex-col gap-4'>
            {manuals.map((m) => (
              <Card key={m.title}>
                <CardContent className='p-6'>
                  <h3 className='text-foreground font-semibold'>{m.title}</h3>
                  <p className='text-muted-foreground mt-1 text-sm'>{m.intro}</p>
                  <ul className='text-muted-foreground mt-4 list-disc space-y-2 pl-5 text-sm'>
                    {m.points.map((pt, i) => (
                      <li key={i}>{pt}</li>
                    ))}
                  </ul>
                </CardContent>
              </Card>
            ))}
          </div>
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
