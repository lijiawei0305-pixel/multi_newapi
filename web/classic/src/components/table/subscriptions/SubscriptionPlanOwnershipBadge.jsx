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

import React from 'react';

import { isSubscriptionPlanReadOnly } from '../../../helpers/subscriptionPlanOwnership';

const SubscriptionPlanOwnershipBadge = ({ record, t }) => {
  if (!isSubscriptionPlanReadOnly(record)) return null;

  const ownerLabel = t('由默认主题的套餐管理维护');
  return (
    <span
      role='status'
      title={ownerLabel}
      aria-label={ownerLabel}
      style={{
        display: 'inline-flex',
        marginTop: 6,
        padding: '2px 8px',
        borderRadius: 9999,
        background: 'var(--semi-color-fill-0)',
        color: 'var(--semi-color-text-2)',
        fontSize: 12,
        lineHeight: '20px',
      }}
    >
      {t('只读')}
    </span>
  );
};

export default SubscriptionPlanOwnershipBadge;
