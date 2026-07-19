---
title: "TOKEN HUB 文档"
source: "https://docs.todoucloud.com/#/"
author:
published:
created: 2026-06-28
description:
tags:
  - "clippings"
---

> [!WARNING]
> **外部历史资料剪藏，不是本项目的可执行运维文档。** 下文保留来源原貌供产品对照；其中
> `auth-service`、`systemctl start auth-service`、旧配置文件路径以及 `/pay/wxpay/notify`、
> `/auth/alipay/notify` 回调均不适用于当前仓库，禁止照抄执行。当前项目采用主站进程内支付和
> `app + mysql + redis` 单栈拓扑，请以 [`deploy/ops/README.md`](deploy/ops/README.md) 与
> [`docs/vendor/payments/deploy-real-payments.md`](docs/vendor/payments/deploy-real-payments.md) 为准。

## TOKEN HUB 操作教程

> 适用版本：TOKEN HUB v1.0（基于 new-api 二次开发）

---

## 目录

## 一、用户注册与登录

### 1.1 注册账号

1. 访问平台 [首页](https://tokenhub.todoucloud.com/) ，点击右上角 [登录](https://tokenhub.todoucloud.com/sign-in) 按钮，切换至 [注册](https://tokenhub.todoucloud.com/sign-up)
2. 填写用户名和密码
3. 如需通过邀请链接注册，请直接点击邀请链接后再注册，系统会自动关联

> 💡 通过代理商邀请链接注册的用户，将成为该代理商的下级用户

### 1.2 登录

1. 点击右上角 **登录**
2. 输入用户名和密码
3. 点击 **登录**

---

## 二、控制台概览

**路径：** 左侧菜单 → 控制台

控制台总览展示：

- **当前余额** ：账户可用的 API 调用额度，用于调用 AI 模型
- **总用量** ：累计消耗的额度
- **请求次数** ：历史 API 请求总数
- **用量图表** ：按时间维度展示 Token 消耗趋势

> 💡 余额说明：
> 
> - **当前余额（API 额度）** ：充值后获得的调用额度，每次调用模型时扣减
> - **可提现余额（仅代理商）** ：从用户充值/消耗中获得的分润收益，可申请提现为现金，与 API 额度相互独立

---

## 三、API Key 管理

![image.png](https://cdn.jsdelivr.net/gh/lizzzy/picBed@master/img/20260527/d8e1787df661168e1805edab6b9f1892.png)

**路径：** 左侧菜单 → 常规 → API 密钥

### 3.1 创建 API Key

1. 点击右上角 **创建 API 密钥**
2. 填写名称（如：我的项目）
3. 可选择设置额度上限（留空表示不限）
4. 点击 **提交**

### 3.2 使用 API Key

获取到的 API Key 格式为 `sk-xxxx` ，接入方式：

```
API 地址：https://tokenhub.todoucloud.com
Authorization: Bearer sk-xxxx
```

完全兼容 OpenAI SDK，只需替换 base\_url 即可：

```python
from openai import OpenAI

client = OpenAI(
    api_key="sk-xxxx",
    base_url="https://tokenhub.todoucloud.com/v1"
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "你好"}]
)
print(response.choices[0].message.content)
```

其他语言示例：

```javascript
// Node.js
import OpenAI from 'openai'

const client = new OpenAI({
  apiKey: 'sk-xxxx',
  baseURL: 'https://tokenhub.todoucloud.com/v1'
})
```
```bash
# cURL
curl https://tokenhub.todoucloud.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"你好"}]}'
```

### 3.3 管理 API Key

- **复制** ：点击 Key 右侧复制图标
- **禁用/启用** ：临时停用某个 Key
- **删除** ：永久删除（不可恢复）
- **编辑** ：修改名称、额度上限、IP 白名单

---

## 四、充值与钱包

![image.png](https://cdn.jsdelivr.net/gh/lizzzy/picBed@master/img/20260527/8e88a02c64f79e1631ad93fa12a0b00e.png)

**路径：** 左侧菜单 → 个人 → 钱包

### 4.1 余额说明

| 余额类型 | 说明 | 用途 |
| --- | --- | --- |
| 当前余额（API 额度） | 充值后的调用额度 | 调用 AI 模型时消耗 |
| 可提现余额（仅代理商） | 分润收益，人民币 | 申请提现打款到账户 |

### 4.2 充值（微信支付）

1. 在充值区域输入充值金额（美元计价，最低 1 美元）
2. 点击 **微信支付** 按钮
3. 弹出微信支付二维码
4. 打开手机微信扫码
5. 确认支付后页面自动关闭，余额即时到账

### 4.3 充值（支付宝）

1. 在充值区域选择或输入充值金额
2. 点击 **支付宝** 按钮
3. 浏览器跳转支付宝支付页面
4. 完成支付后余额自动到账

### 4.4 充值金额与梯度折扣

预设充值档位（美元），对应实际人民币支付金额如下（以汇率和折扣为准）：

| 充值额度 | 说明 |
| --- | --- |
| $10 | 基础档位 |
| $20 | 部分档位享有折扣 |
| $50 及以上 | 享有更高梯度折扣 |

> 💡 实际支付金额以页面显示的 Pay ¥xxx 为准

### 4.5 使用兑换码

1. 在钱包页面下方找到 **有兑换码吗？** 区域
2. 输入兑换码
3. 点击 **兑换额度** 完成兑换

### 4.6 充值记录

点击钱包页面右上角 **订单历史** 查看历史充值记录

### 4.7 代理商提现

> 仅代理商账号可见

![image.png](https://cdn.jsdelivr.net/gh/lizzzy/picBed@master/img/20260527/79d660a9a02713f2a772f64ab81b2510.png)

**可提现余额** = 代理商从旗下用户充值差价 + 用户 API 消耗分润中累计获得的收益（人民币）

提现流程：

1. 在钱包页面找到 **代理商钱包** 卡片
2. 查看「可提现余额」和「累计收益」
3. 点击 **申请提现**
4. 填写提现金额（不超过可提现余额）
5. 选择收款方式：支付宝 / 微信 / 银行卡
6. 填写收款人姓名和账号
7. 点击 **确认提交**
8. 系统自动预扣余额，等待管理员审核打款

> ⚠️ 提现时会收取一定比例的手续费，实际到账金额以页面显示为准

---

## 五、AI 对话

**路径：** 左侧菜单 → 聊天 → 游乐场

### 5.1 基本使用

1. 在左侧选择模型（如：gpt-4o、claude-sonnet-4-6）
2. 在输入框输入消息
3. 点击发送或按 Enter
4. 右侧显示 AI 回复

### 5.2 系统提示词（System Prompt）

在对话框上方可设置系统提示词，定义 AI 的角色和行为。

### 5.3 参数调节

| 参数 | 说明 |
| --- | --- |
| Temperature | 回复随机性，越高越有创意（0-2） |
| Max Tokens | 单次回复最大 Token 数 |
| Top P | 核采样参数 |

### 5.4 对话管理

- **新建对话** ：点击左上角 + 号
- **历史记录** ：左侧列表展示历史对话
- **清空对话** ：点击清空按钮重置上下文

> 💡 Playground 消耗的 Token 会从账户余额扣除，可在使用日志中查看详情

---

## 六、使用日志

**路径：** 左侧菜单 → 常规 → 使用日志

日志记录每次 API 请求的详细信息：

| 字段 | 说明 |
| --- | --- |
| 请求时间 | 精确到秒 |
| 模型 | 调用的 AI 模型名称 |
| 输入 Token | 提示词消耗 |
| 输出 Token | 回复消耗 |
| 消耗额度 | 本次请求扣费金额 |
| 请求来源 | 哪个 API Key 发起 |
| 渠道 | 上游服务商 |

**筛选功能：**

- 按时间范围筛选
- 按模型筛选
- 按 API Key 筛选
- 关键词搜索

---

## 七、代理商功能

> 以下功能仅代理商账号可见（左侧菜单 **OEM 管理** 分组）

### 7.1 品牌配置

![image.png](https://cdn.jsdelivr.net/gh/lizzzy/picBed@master/img/20260527/b16c56a0f55b877c71ed4df5980e01e4.png)

**路径：** OEM 管理 → 品牌配置

分五个 Tab 配置：

**品牌 Tab：**

| 配置项 | 说明 |
| --- | --- |
| 站点名称 | 显示在浏览器标题和顶部导航 |
| Logo 地址 | 填写 Logo 图片 URL |
| 页脚版权 | 页面底部版权文字 |
| 自定义域名 | 填写你的域名（不含 https://） |

**联系 Tab：**

| 配置项 | 说明 |
| --- | --- |
| 联系邮箱 | 显示在页脚和联系悬浮按钮 |
| 手机号 | 联系悬浮按钮显示 |
| 微信号 | 联系悬浮按钮显示 |
| 微信二维码图片地址 | 鼠标悬停显示二维码 |
| QQ 号 | 联系悬浮按钮显示 |
| 文档链接 | 顶部导航 Docs 链接 |

**充值 Tab：**

| 配置项 | 说明 |
| --- | --- |
| 充值链接 | 用户点击充值时跳转的地址 |
| 充值说明 | 充值页面显示的说明文字 |

**内容 Tab：**

| 配置项 | 说明 |
| --- | --- |
| 首页公告 | 支持 Markdown，新公告自动弹窗一次 |
| 关于我们 | 关于页面内容 |
| SEO 标题 | 搜索引擎显示的标题 |
| SEO 描述 | 搜索引擎显示的描述 |

**协议 Tab：**

| 配置项 | 说明 |
| --- | --- |
| 用户协议 | 支持 Markdown 或 HTML |
| 隐私政策 | 支持 Markdown 或 HTML |

> 💡 配置完成后点击右上角 **保存配置** 按钮，立即生效

### 7.2 首页定制

**路径：** OEM 管理 → 品牌配置 → 首页 Tab

选择首页模式：

- **默认** ：使用系统默认首页
- **配置模式** ：填写 Banner JSON 数组自定义轮播内容
- **自定义 HTML** ：上传完整 HTML 页面

Banner JSON 示例：

```json
[
  {
    "id": "banner1",
    "badge": "AI 平台",
    "title": "我的 AI 平台",
    "subtitle": "高性价比 · 稳定可靠",
    "desc": "聚合主流 AI 模型，企业级可用",
    "image": "https://example.com/banner.jpg",
    "primaryText": "立即体验",
    "primaryTo": "/sign-up",
    "secondaryText": "查看定价",
    "secondaryTo": "/pricing",
    "tags": ["GPT-4", "Claude", "DeepSeek"]
  }
]
```

### 7.3 绑定自定义域名

1. 在品牌配置 → 品牌 Tab 填写 **自定义域名** （如： `ai.example.com` ）
2. 点击保存
3. 在域名 DNS 服务商添加 A 记录，指向服务器 IP： `192.140.178.86`
4. 联系管理员配置 SSL 证书
5. 完成后访问你的域名即可看到品牌化站点

### 7.4 用户组管理

![image.png](https://cdn.jsdelivr.net/gh/lizzzy/picBed@master/img/20260527/df07c140bf1ec5a4583a3b250d5337f2.png)

**路径：** OEM 管理 → 我的用户组

**倍率说明：**

| 倍率 | 含义 | 举例 |
| --- | --- | --- |
| 1.0 | 正常价格（与平台相同） | 充 $10 付 ¥100 |
| 0.9 | 用户享受九折优惠 | 充 $10 付 ¥90 |
| 1.5 | 用户溢价 50%（代理商盈利） | 充 $10 付 ¥150 |

> ⚠️ 倍率越大，用户充值同等金额获得的额度越少；代理商可通过设置高于成本价的倍率赚取差价

**修改分组倍率：**

1. 在已有用户组右侧输入新倍率
2. 点击 **保存**

**创建新分组：**

1. 点击右上角 **新建分组**
2. 填写分组名称
3. 设置初始倍率
4. 点击 **确认创建**

> 💡 创建的分组名称会自动加上 `agent{你的ID}_` 前缀，避免和系统分组冲突

### 7.5 代理商收益模式

TOKEN HUB 支持两种代理商盈利方式，可同时开启：

**方式一：充值差价**

- 用户按代理商设置的分组倍率充值
- 代理商按成本价进货
- 差价自动计入代理商可提现余额

示例：成本价 ¥8/刀，用户倍率 1.5，用户充 $10 付 ¥150，代理商成本 ¥80，差价 ¥70

**方式二：消耗分润**

- 用户每次调用 API 消耗额度时
- 系统按设定比例给代理商分润
- 比例由超级管理员在系统设置中配置

**关闭方式：**

- 充值差价：将代理商成本价设为 0
- 消耗分润：将分润比例设为 0

### 7.6 兑换码管理

**路径：** OEM 管理 → 兑换码

> ⚠️ 创建兑换码会从代理商自己的 API 额度中预扣，请确保账户有足够余额

**创建兑换码：**

1. 点击 **创建兑换码**
2. 填写名称（如：新用户体验码）
3. 填写额度（元为单位）
4. 填写数量（最多 100 个）
5. 可设置过期时间，或选择快捷按钮（永不 / 1天 / 1周 / 1个月）
6. 点击 **确认创建**

**兑换码状态说明：**

| 状态 | 说明 |
| --- | --- |
| 启用 | 可正常使用 |
| 禁用 | 已手动停用，不可兑换 |
| 已使用 | 已被用户兑换，不可再次启用 |

### 7.7 我的用户

![image.png](https://cdn.jsdelivr.net/gh/lizzzy/picBed@master/img/20260527/be82e429e717e6d0772fe11567daf11b.png)

**路径：** OEM 管理 → 我的用户

展示通过推广链接/代理域名注册的所有用户：

| 列 | 说明 |
| --- | --- |
| 余额 | 用户当前可用额度（元） |
| 已消耗 | 用户累计消耗额度（元） |
| 邀请渠道 | 推广计划 / 推广渠道 / 代理域名 |
| 状态 | 正常 / 禁用 |

**用户操作：**

- 🚫 禁用：禁止用户使用
- ✅ 启用：恢复用户访问
- 🗑️ 删除：软删除用户
- ✏️ 修改分组：将用户移至指定分组

**顶部统计：**

- 邀请用户数、用户总余额、用户总消耗

### 7.8 推广渠道

**路径：** OEM 管理 → 推广渠道

**创建推广渠道：**

1. 点击 **新建渠道**
2. 填写渠道名称（如：微信朋友圈）
3. 选择或输入渠道前缀（英文，如：wechat、xiaohongshu、douyin）
4. 点击 **确认创建**

系统生成专属推广链接，格式为：

```
https://你的域名/sign-up?channel=wechat_xxxxxx
```

**分享方式建议：**

| 渠道 | 前缀建议 | 推广技巧 |
| --- | --- | --- |
| 微信朋友圈 | wechat | 附上平台介绍和链接 |
| 小红书 | xiaohongshu | 发布使用心得笔记 |
| 抖音 | douyin | 录制 AI 使用教程视频 |
| 微博 | weibo | 分享到话题讨论 |
| B站 | bilibili | 发布测评视频 |

### 7.9 开放 API（仅 API 代理类型）

**路径：** OEM 管理 → 开放 API

为合作方提供 AI 能力接入，通过 API 密钥管理用户和额度：

**生成 API 密钥：**

1. 点击 **生成新密钥**
2. 复制密钥妥善保存

**接口说明：**

| 接口 | 说明 |
| --- | --- |
| POST /api/open/token/create | 创建用户 Token（幂等） |
| GET /api/open/token/:id | 查询 Token 信息 |
| POST /api/open/token/recharge | 给 Token 充值额度 |
| DELETE /api/open/token/:id | 删除 Token |
| GET /api/open/tokens | 列出所有 Token |

---

## 八、超级管理员功能

> 以下功能仅超级管理员账号可见（左侧菜单 **Admin** 分组）

### 8.1 渠道管理

![image.png](https://cdn.jsdelivr.net/gh/lizzzy/picBed@master/img/20260527/778d92a541d22d33816a6177df3aa717.png)

**路径：** 管理员 → 渠道

渠道即 AI 模型的上游接口配置：

**添加渠道：**

1. 点击 **添加渠道**
2. 选择渠道类型（OpenAI / Anthropic / 自定义等）
3. 填写 API Key
4. 配置模型列表（从模型广场选择）
5. 设置分组权限（不填表示所有分组可用）
6. 设置优先级（数字越大优先级越高）和权重（同优先级下按权重分流）
7. 点击 **提交**

**渠道状态：**

- 🟢 正常：可正常调用
- 🔴 异常：自动禁用，需排查
- 点击 **测试** 按钮可手动检测渠道可用性

### 8.2 模型管理

**路径：** 管理员 → 模型

管理模型广场的展示信息和分组权限。

### 8.3 用户管理

**路径：** 管理员 → 用户

**查看用户：**

- 支持按用户名、邮箱搜索
- 可查看每个用户的余额、已用额度、请求次数

**用户操作：**

- **编辑** ：修改用户名、邮箱、分组、额度
- **禁用** ：禁止用户登录和调用
- **启用** ：恢复用户
- **删除** ：永久删除

### 8.4 兑换码管理

**路径：** 管理员 → 兑换码

系统级兑换码管理，创建时从管理员账户扣除对应额度。

### 8.5 子代理管理

![image.png](https://cdn.jsdelivr.net/gh/lizzzy/picBed@master/img/20260527/789edc31fb09ce3148bf26404e455b7f.png)

**路径：** 管理员 → 子代理管理

**设置子代理：**

1. 点击 **添加子代理**
2. 搜索用户名或邮箱
3. 选择代理类型（普通代理 / OEM代理 / API代理）
4. 点击 **设为代理**

**代理类型说明：**

| 类型 | 功能 |
| --- | --- |
| 普通代理 | 基础推广功能，管理用户分组和兑换码 |
| OEM代理 | 完整品牌定制，自定义域名、Logo、首页等 |
| API代理 | 面向合作方，提供开放 API 接入能力 |

**代理商参数配置：**

| 参数 | 说明 |
| --- | --- |
| 成本价 | 代理商充值时的进货价（元/刀），影响充值差价分润 |
| 套餐折扣 | 代理商购买订阅套餐时的折扣 |
| 钱包余额 | 代理商当前可提现余额 |
| 等级 | 套用预设等级自动填入成本价和折扣 |

**等级管理：**

1. 点击 **等级管理** 按钮
2. 新增等级（如：金牌、银牌）
3. 设置对应的成本价、套餐折扣、消耗分润比例
4. 在代理商列表中为代理商选择对应等级，自动套用参数

**提现管理：**

1. 点击右上角 **提现管理**
2. 查看所有待审核提现申请
3. 填写管理员备注（可选）
4. 点击 **通过** 或 **拒绝**
5. 通过后系统已预扣余额，管理员手动打款给代理商

### 8.6 推广渠道（管理员）

**路径：** 管理员 → 推广渠道

管理员创建平台级推广渠道，统计全平台推广数据。

---

## 九、系统设置

![image.png](https://cdn.jsdelivr.net/gh/lizzzy/picBed@master/img/20260527/fc3b1d8faa7d05fbbcc6d42cd6fe7168.png)

**路径：** 设置 → 系统设置

### 9.1 系统信息

| 配置项 | 说明 |
| --- | --- |
| 系统名称 | 显示在标题栏 |
| 站点描述 | 页脚站点描述 |
| 服务器地址 | 用于支付回调等 |
| 徽标 URL | 系统 Logo 图片地址 |
| 页脚 | 自定义页脚 HTML |
| 关于 | 关于页面内容 |
| 用户协议 | 留空则不显示链接 |
| 隐私政策 | 留空则不显示链接 |

### 9.2 计费与支付

**额度设置：**

| 配置项 | 说明 |
| --- | --- |
| 新用户初始额度 | 注册后自动发放的免费额度 |
| 邀请人奖励 | 成功邀请用户后获得的额度 |
| 被邀请人奖励 | 通过邀请链接注册后获得的额度 |
| 推广返佣比例 | 用户充值时给邀请人返佣的比例（0-1） |
| 代理消耗分润比例 | 用户消耗 API 时给代理商的分润比例（0-1） |
| 提现手续费比例 | 代理商提现时收取的手续费（0-1） |

**模型定价：**

- 设置各模型的输入/输出 Token 单价
- 支持重置为官方标准价格

**货币设置：**

| 配置项 | 说明 |
| --- | --- |
| 显示货币类型 | CNY（人民币）/ USD（美元）/ 自定义 |
| USD 汇率 | 美元兑人民币汇率 |
| 自定义货币符号 | 使用自定义货币时的符号 |

### 9.3 系统公告

- 填写公告内容（支持 Markdown）
- 保存后首页自动弹窗提示（每个公告只弹一次）

---

## 十、合作方式与部署

### 10.1 合作方式

TOKEN HUB 提供以下合作方式：

| 方式 | 说明 | 适合对象 |
| --- | --- | --- |
| 托管代理 | 在主站注册代理账号，直接使用现有平台 | 个人推广、小团队 |
| OEM 品牌定制 | 自定义域名、Logo、首页，独立品牌运营 | 有品牌需求的团队 |
| 源代码部署 | 完整源码部署，完全独立运营 | 企业、技术团队 |

---

### 10.2 源代码部署

#### 服务器要求

| 配置项 | 最低要求 | 推荐配置 |
| --- | --- | --- |
| CPU | 2 核 | 4 核及以上 |
| 内存 | 4 GB | 8 GB 及以上 |
| 硬盘 | 40 GB SSD | 100 GB SSD |
| 操作系统 | Ubuntu 20.04 LTS | Ubuntu 22.04 LTS |
| 带宽 | 5 Mbps | 10 Mbps 及以上 |
| 公网 IP | 必须 | 必须 |

> 💡 推荐使用阿里云、腾讯云、华为云等国内云服务器，确保低延迟和稳定性

#### 软件依赖

| 软件 | 版本要求 | 用途 |
| --- | --- | --- |
| Go | 1.21+ | 后端编译 |
| Node.js | 18+ | 前端构建 |
| MySQL | 8.0+ | 数据存储 |
| Nginx | 1.18+ | 反向代理 |
| 宝塔面板（可选） | 最新版 | 服务器管理 |

#### 域名要求

需要准备以下域名（均需备案）：

| 域名用途 | 示例 | 说明 |
| --- | --- | --- |
| 主域名 | tokenhub.yourdomain.com | 平台主站 |
| 代理商域名（可选） | agent1.yourdomain.com | OEM 代理商独立站 |
| 文档域名（可选） | docs.yourdomain.com | 平台文档站 |

> ⚠️ 国内服务器必须完成 ICP 备案，域名需实名认证

#### 部署步骤概览

```bash
# 1. 克隆源码
git clone https://github.com/your-repo/token-hub.git
cd token-hub

# 2. 编译后端
go build -o new-api .

# 3. 构建前端
cd web/default
yarn install && yarn build

# 4. 配置数据库
mysql -u root -p < schema.sql

# 5. 启动服务
systemctl start new-api
systemctl start auth-service
```

---

### 10.3 开通微信支付

#### 申请前提条件

- 已注册微信支付商户号（需企业营业执照）
- 企业银行账户
- 法人身份证

#### 申请步骤

1. 访问 [微信支付商户平台](https://pay.weixin.qq.com/) 注册商户号
2. 完成企业资质认证
3. 在商户平台 → 产品中心 → 开发配置 中获取：
	- `商户号（mch_id）`
		- `APIv3 密钥（v3_key）`
4. 在商户平台下载 API 证书：
	- `apiclient_key.pem` （商户私钥）
5. 申请微信支付公钥，获取：
	- `pub_key.pem` （微信支付公钥）
		- `公钥 ID（pub_key_id）`

#### 配置到系统

将以上信息填入服务器配置文件 `/www/wwwroot/auth-service/config/config.yaml` ：

```yaml
wxpay:
  mch_id: "你的商户号"
  app_id: "你的AppID"
  mch_cert_serial_num: "证书序列号"
  private_key_path: "/path/to/apiclient_key.pem"
  pub_key_path: "/path/to/pub_key.pem"
  pub_key_id: "公钥ID"
  v3_key: "APIv3密钥"
```

在微信支付商户平台配置回调地址：

```
https://你的域名/pay/wxpay/notify
```

---

### 10.4 开通支付宝支付

#### 申请前提条件

- 企业支付宝账号
- 企业营业执照
- 已在 [支付宝开放平台](https://open.alipay.com/) 创建应用

#### 申请步骤

1. 登录 [支付宝开放平台](https://open.alipay.com/)
2. 创建网页/移动应用
3. 申请开通 **电脑网站支付** 能力
4. 在应用详情 → 开发设置中：
	- 配置 **应用公钥** （使用 RSA2 算法）
		- 获取 **支付宝公钥**
		- 下载证书模式所需的三个文件：
		- `app_private_key.pem` （应用私钥）
				- `appCertPublicKey.crt` （应用公钥证书）
				- `alipayCertPublicKey_RSA2.crt` （支付宝公钥证书）
5. 配置回调地址（异步通知 URL）：
	```
	https://你的域名/auth/alipay/notify
	```

#### 配置到系统

将证书上传到服务器并配置：

```yaml
alipay:
  app_id: "你的AppID"
  private_key_path: "/path/to/app_private_key.pem"
  app_cert_path: "/path/to/appCertPublicKey.crt"
  root_cert_path: "/path/to/appCertPublicKey.crt"
  public_cert_path: "/path/to/alipayCertPublicKey_RSA2.crt"
  notify_url: "https://你的域名/auth/alipay/notify"
  is_prod: true
```

> ⚠️ 支付宝审核通常需要 1-3 个工作日，审核期间可使用沙箱环境测试

---

### 10.5 Nginx 配置示例

```nginx
server {
    listen 443 ssl;
    server_name tokenhub.yourdomain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    # 主应用
    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }

    # 支付服务
    location ^~ /pay/ {
        proxy_pass http://127.0.0.1:8080/pay/;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    # 支付宝回调
    location ^~ /auth/ {
        proxy_pass http://127.0.0.1:8080/auth/;
        proxy_set_header Host $http_host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

---

## 附：快捷链接

| 功能 | 路径 |
| --- | --- |
| 控制台 | /dashboard |
| API Keys | /keys |
| 钱包 | /wallet |
| AI 对话 | /playground |
| 模型广场 | /pricing |
| 关于页面 | /about |
| 用户协议 | /user-agreement |
| 隐私政策 | /privacy-policy |
| 代理加盟 | /affiliate |
| 品牌配置 | /agent/brand |
| 我的用户组 | /agent/groups |
| 我的用户 | /agent/users |
| 推广渠道 | /agent/channels |
| 兑换码管理 | /agent/redemptions |
| 开放 API | /agent/open-api |
| 用户管理 | /users |
| 渠道管理 | /channels |
| 子代理管理 | /admin/agents |
| 系统设置 | /system-settings/site |
| 计费设置 | /system-settings/billing/quota |
| 模型定价 | /system-settings/billing/model-pricing |

---

*文档版本：2026-06-08 | TOKEN HUB v1.0*
