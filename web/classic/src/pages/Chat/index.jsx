/*
Copyright (C) 2025 QuantumNous

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

import React, { useMemo } from 'react';
import { useTokenKeys } from '../../hooks/chat/useTokenKeys';
import { Spin } from '@douyinfe/semi-ui';
import { useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { resolveChatTemplateUrl } from '../../helpers/safeNavigation';

const ChatPage = () => {
  const { t } = useTranslation();
  const { id } = useParams();
  const { keys, serverAddress, isLoading } = useTokenKeys(id);

  const iframeSrc = useMemo(() => {
    const chatIndex = Number(id);
    if (!Number.isInteger(chatIndex) || chatIndex < 0 || !keys[0]) return '';
    try {
      const chats = JSON.parse(localStorage.getItem('chats') || '[]');
      const chat = Array.isArray(chats) ? chats[chatIndex] : null;
      if (!chat || typeof chat !== 'object' || Array.isArray(chat)) return '';
      const template = Object.values(chat).find(
        (value) => typeof value === 'string',
      );
      return (
        resolveChatTemplateUrl(template, serverAddress, keys[0], {
          webOnly: true,
        }) || ''
      );
    } catch {
      return '';
    }
  }, [id, keys, serverAddress]);

  if (!isLoading && !iframeSrc) {
    return (
      <div className='fixed inset-0 mt-[60px] flex h-screen w-screen items-center justify-center bg-white/80'>
        <span style={{ color: 'var(--semi-color-warning)' }}>
          {t('请联系管理员配置聊天链接')}
        </span>
      </div>
    );
  }

  return !isLoading && iframeSrc ? (
    <iframe
      src={iframeSrc}
      style={{
        width: '100%',
        height: 'calc(100vh - 64px)',
        border: 'none',
        marginTop: '64px',
      }}
      title='Token Frame'
      allow='camera;microphone'
      sandbox='allow-downloads allow-forms allow-popups allow-scripts'
      referrerPolicy='no-referrer'
    />
  ) : (
    <div className='fixed inset-0 w-screen h-screen flex items-center justify-center bg-white/80 z-[1000] mt-[60px]'>
      <div className='flex flex-col items-center'>
        <Spin size='large' spinning={true} tip={null} />
        <span
          className='whitespace-nowrap mt-2 text-center'
          style={{ color: 'var(--semi-color-primary)' }}
        >
          {t('正在跳转...')}
        </span>
      </div>
    </div>
  );
};

export default ChatPage;
