/* 在线客服弹窗 —— 移植自 bulb-orbit/index.html（HTML 3112–3129 + 脚本 lpOpenKefu/lpCloseKefu 3131–3133）。
   与原版差异：开关状态由 React state 驱动（取代 classList.add('show')），Esc 监听在卸载时解绑。
   文案走 New API 官方 i18n（key = 英文原句，译文在 src/i18n/locales/*.json）。
   二维码沿用原版的占位块；用户给到真图后，把 .lp-kefu-qr 内容换成 <img src='/lp-assets/kefu-qr.png' /> 即可。 */
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

const IcHeadset = () => (
  <svg viewBox='0 0 24 24'>
    <path d='M4 14v-2a8 8 0 0 1 16 0v2' />
    <path d='M4 14a2 2 0 0 1 2-2h1v5H6a2 2 0 0 1-2-2v-1ZM20 14a2 2 0 0 0-2-2h-1v5h1a2 2 0 0 0 2-2v-1Z' />
    <path d='M18 17v1a3 3 0 0 1-3 3h-3' />
  </svg>
)

export function KefuModal({
  open,
  onClose,
}: {
  open: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  return (
    <div
      className={`lp-kefu-modal${open ? ' show' : ''}`}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div className='lp-kefu-card'>
        <div className='lp-kefu-head'>
          <span className='lp-kh-ic'>
            <IcHeadset />
          </span>
          <div>
            <b>{t('Live Support')}</b>
            <small>
              {t('AI bot + human agents, standing by around the clock')}
            </small>
          </div>
          <button
            className='lp-kefu-close'
            type='button'
            onClick={onClose}
            aria-label={t('Close')}
          >
            ×
          </button>
        </div>

        <div className='lp-kefu-body'>
          <div className='lp-kefu-orb'>
            <IcHeadset />
          </div>
          <div className='lp-kefu-title'>{t('24/7 Live Support')}</div>
          <div className='lp-kefu-status'>{t('Online 24/7')}</div>
          <div className='lp-kefu-qr'>
            <div className='lp-kefu-qr-ph'>
              <svg viewBox='0 0 24 24' strokeLinejoin='round'>
                <rect x='3' y='3' width='7' height='7' rx='1' />
                <rect x='14' y='3' width='7' height='7' rx='1' />
                <rect x='3' y='14' width='7' height='7' rx='1' />
                <path d='M14 14h3v3M20 14v7M14 20h3' />
              </svg>
              <span>
                {t('WeChat QR')}
                <br />
                {t('placeholder · to be replaced')}
              </span>
            </div>
          </div>
          <div className='lp-kefu-hint'>
            {t('(Scan the QR code with WeChat)')}
          </div>
        </div>

        {/* 原版此按钮 onclick=lpOpenKefu()，弹窗已开时为空操作 —— 保持一致，等真实客服入口再接。 */}
        <button className='lp-kefu-contact' type='button'>
          <IcHeadset />
          {t('Contact support now')}
        </button>
      </div>
    </div>
  )
}
