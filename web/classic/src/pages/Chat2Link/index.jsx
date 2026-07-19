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

import React, { useEffect, useRef, useState } from 'react';
import { useTokenKeys } from '../../hooks/chat/useTokenKeys';
import { useTranslation } from 'react-i18next';
import {
  assignHttpNavigationUrl,
  findFirstSafeWebChatTemplate,
  resolveChatTemplateUrl,
} from '../../helpers/safeNavigation';

const Chat2Page = () => {
  const { t } = useTranslation();
  const { keys, serverAddress, isLoading } = useTokenKeys();
  const redirectedRef = useRef(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (redirectedRef.current || isLoading || !keys[0] || !serverAddress) {
      return;
    }
    try {
      const chats = JSON.parse(localStorage.getItem('chats') || '[]');
      const template = findFirstSafeWebChatTemplate(chats);
      const redirectLink = resolveChatTemplateUrl(
        template,
        serverAddress,
        keys[0],
        { webOnly: true },
      );
      if (redirectLink && assignHttpNavigationUrl(redirectLink)) {
        redirectedRef.current = true;
        return;
      }
      setFailed(true);
    } catch {
      setFailed(true);
    }
  }, [isLoading, keys, serverAddress]);

  return (
    <div className='mt-[60px] px-2'>
      <h3>{failed ? t('请联系管理员配置聊天链接') : t('正在跳转...')}</h3>
    </div>
  );
};

export default Chat2Page;
