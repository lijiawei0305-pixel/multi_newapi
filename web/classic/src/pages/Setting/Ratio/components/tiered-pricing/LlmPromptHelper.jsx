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

import React, { useCallback, useMemo, useState } from 'react';
import {
  Button,
  Card,
  Collapsible,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconCopy } from '@douyinfe/semi-icons';
import { copy, showSuccess } from '../../../../../helpers';

const { Text } = Typography;

const LLM_PROMPT_TEMPLATE = `你是一个 AI API 计费表达式设计助手。用户需要你帮忙设计一个计费表达式（billing expression），用于 AI API 网关的模型计费。

## 表达式语言

表达式基于 expr-lang/expr，支持标准算术运算和三元运算符。

### Token 变量

输入侧：
- p — 输入 token 数（计价用）。系统会自动排除表达式中单独计价的子类别（如用了 cr，缓存 token 就从 p 中扣除）
- len — 输入上下文总长度（条件判断用）。不受自动排除影响，始终反映完整输入长度。用于阶梯条件判断
- cr — 缓存命中（读取）token 数
- cc — 缓存创建 token 数（5分钟 TTL）
- cc1h — 缓存创建 token 数（1小时 TTL，Claude 专用）
- img — 图片输入 token 数
- ai — 音频输入 token 数

输出侧：
- c — 输出 token 数。同样会自动排除单独计价的子类别
- img_o — 图片输出 token 数
- ao — 音频输出 token 数

### p/c 自动排除机制

p 和 c 是兜底变量，代表所有没有被表达式单独定价的 token。如果表达式使用了某个子类别变量（如 cr），对应 token 就从 p 中扣除，避免重复计费。没用到的子类别 token 则留在 p/c 中按基础价格计费。

重要：len 不受自动排除影响。阶梯条件应使用 len 而非 p，以避免缓存命中导致 p 降低而误判档位。

### 内置函数

- tier(name, value) — 标记计费档位名称，必须包裹费用表达式
- max(a, b)、min(a, b) — 取大/小值
- ceil(x)、floor(x)、abs(x) — 向上取整、向下取整、绝对值
- header(name) — 读取请求头
- param(path) — 读取请求体 JSON 路径（gjson 语法）
- has(source, substr) — 子字符串检查
- hour(tz)、minute(tz)、weekday(tz)、month(tz)、day(tz) — 时间函数，tz 为时区如 "Asia/Shanghai"

### 价格系数

表达式中的数字系数是 $/1M tokens 的价格。例如 p * 2.5 表示输入 $2.50/1M tokens。

## 表达式示例

简单定价：
tier("base", p * 2.5 + c * 15)

带缓存的定价：
tier("base", p * 2.5 + c * 15 + cr * 0.25)

多档阶梯（用 len 做条件）：
len <= 200000
  ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)
  : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12)

图片模型：
tier("base", p * 2 + c * 8 + img * 2.5)

多模态含音频：
tier("base", p * 0.43 + c * 3.06 + img * 0.78 + ai * 3.81 + ao * 15.11)

三档阶梯示例：
len <= 128000
  ? tier("standard", p * 1.1 + c * 4.4)
  : (len <= 1000000
    ? tier("medium", p * 2.2 + c * 8.8)
    : tier("long", p * 4.4 + c * 17.6))

## 规则

1. 每个叶子分支必须用 tier("名称", 费用表达式) 包裹
2. tier 名称用英文，如 "base"、"standard"、"long_context"
3. 阶梯条件用 len（不要用 p），支持 <、<=、>、>=
4. 多档用嵌套三元运算符：条件1 ? tier(...) : (条件2 ? tier(...) : tier(...))
5. 价格系数直接写供应商官方 $/1M tokens 价格
6. 不需要缓存/图片/音频单独定价时可以不写对应变量，它们的 token 会自动包含在 p/c 中

请根据用户提供的模型信息和定价需求，生成计费表达式。`;

export default function LlmPromptHelper({ t, model }) {
  const [open, setOpen] = useState(false);

  const modelName = model?.name || '';
  const prompt = useMemo(() => {
    if (modelName) {
      return LLM_PROMPT_TEMPLATE + `\n\n当前模型：${modelName}`;
    }
    return LLM_PROMPT_TEMPLATE;
  }, [modelName]);

  const handleCopy = useCallback(async () => {
    const ok = await copy(prompt);
    if (ok) showSuccess(t('已复制到剪贴板'));
  }, [prompt, t]);

  return (
    <div style={{ marginBottom: 12 }}>
      <Button
        theme='borderless'
        size='small'
        icon={<IconCopy />}
        onClick={() => setOpen(!open)}
        style={{ color: 'var(--semi-color-tertiary)' }}
      >
        {t('LLM 辅助设计提示词')}
      </Button>
      <Collapsible isOpen={open}>
        <Card
          bodyStyle={{ padding: 12 }}
          style={{ marginTop: 8, background: 'var(--semi-color-fill-0)' }}
        >
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              marginBottom: 8,
            }}
          >
            <Text size='small' type='secondary'>
              {t(
                '复制以下提示词发送给 LLM（如 ChatGPT / Claude），让它帮你设计计费表达式',
              )}
            </Text>
            <Button
              icon={<IconCopy />}
              size='small'
              theme='light'
              onClick={handleCopy}
            >
              {t('复制提示词')}
            </Button>
          </div>
          <TextArea
            value={prompt}
            readonly
            autosize={{ minRows: 6, maxRows: 20 }}
            style={{ fontFamily: 'monospace', fontSize: 12 }}
          />
        </Card>
      </Collapsible>
    </div>
  );
}
