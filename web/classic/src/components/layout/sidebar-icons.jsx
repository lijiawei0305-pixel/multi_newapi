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

import {
  BarChart3,
  CalendarClock,
  CheckSquare,
  CircleUser,
  CreditCard,
  Gift,
  Image as ImageIcon,
  Key,
  Layers,
  LayoutDashboard,
  MessageSquare,
  Package,
  Server,
  Settings,
  TerminalSquare,
  User,
} from 'lucide-react';

export function getSidebarIcon(key, selected = false) {
  const commonProps = {
    size: 16,
    strokeWidth: 2,
    color: selected ? 'var(--semi-color-primary)' : 'currentColor',
    className: `transition-colors duration-200 ${selected ? 'transition-transform duration-200 scale-105' : ''}`,
  };

  switch (key) {
    case 'detail':
      return <LayoutDashboard {...commonProps} />;
    case 'playground':
      return <TerminalSquare {...commonProps} />;
    case 'chat':
      return <MessageSquare {...commonProps} />;
    case 'token':
      return <Key {...commonProps} />;
    case 'log':
      return <BarChart3 {...commonProps} />;
    case 'midjourney':
      return <ImageIcon {...commonProps} />;
    case 'task':
      return <CheckSquare {...commonProps} />;
    case 'topup':
      return <CreditCard {...commonProps} />;
    case 'channel':
      return <Layers {...commonProps} />;
    case 'redemption':
      return <Gift {...commonProps} />;
    case 'user':
    case 'personal':
      return <User {...commonProps} />;
    case 'models':
      return <Package {...commonProps} />;
    case 'deployment':
      return <Server {...commonProps} />;
    case 'subscription':
      return <CalendarClock {...commonProps} />;
    case 'setting':
      return <Settings {...commonProps} />;
    default:
      return <CircleUser {...commonProps} />;
  }
}
