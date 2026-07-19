# 落地页 Hero 三维轨道 & 全息模型改造 — 设计规格

> 状态:待用户过目 · 日期 2026-07-15
> 作用域:仅主站 React 落地页 Hero 视觉(`web/default/src/features/landing-react/`)。
> 参考源:同学项目 [`github.com/wei500L/yun`](https://github.com/wei500L/yun) 的 `src/main.js`(已完整研读)。

---

## 1. 目标与背景

**用户诉求:** 现在这版 React 落地页的**轨道**和**模型外观**不满意,要换成 yun 的做法 —— yun 的轨道是真三维、卫星是"3D 全息带 JS 动效"的模型。灯泡当初就是从 yun 抄的,现在把轨道和模型也对齐,并加一批新的运动/交互/配色要求。

**当前实现(要被替换的部分):**
| 部件 | 现状 | 文件 |
| --- | --- | --- |
| 灯泡 | WebGL 粒子点云(源自 yun),**单独一个定尺 940px 画布** | `bulb3d.ts` |
| 轨道 | **SVG 平面**,64 段 `<line>` 拼伪 3D 环 | `orbit.ts` |
| 卫星模型 | **DOM 平面 logo**(`.chip`),CSS 自转 | `orbit.ts` + `landing-css.ts` |
| HUD | DOM `#hud` 卡片,点击选中填充 | `index.tsx` + `orbit.ts` |

**yun 的实现(要对齐的目标):** 灯泡 + 轨道 + 卫星**全在一个 three.js 场景**里,真三维相机 + 共享辉光(UnrealBloom)+ 深度缓冲。轨道是倾斜的 `TubeGeometry` 管环,带自定义着色器(深度淡化、灯泡剪影遮罩、**彗尾光迹**、能量流)。卫星是三层结构:品牌色粒子点云光晕 + 微光背/金属环 + **始终朝向相机的 logo 图**。

---

## 2. 技术路线(及被否方案)

**采用:统一 three.js 3D 场景。** 以 yun 的 `main.js` 为骨架改造,把灯泡、两条 3D 轨道、卫星装进**同一个 WebGL 画布**,挂进 React 落地页的 `.hero-right` 区域(适配盒子尺寸,**非全窗口**),保留现有两栏布局、中文 i18n HUD、W5 中文纪律。

**为什么必须统一场景:** 用户的核心需求之一是"图标绕到灯泡**前后**时自动被遮挡 / 缩放 / 变暗"。只有灯泡与轨道/卫星处于**同一深度空间**(共享相机与 z-buffer),这个前后遮挡才天然成立。

**否掉的备选:** 保留现在单独的灯泡画布,只在其上叠一层轨道/卫星画布。两者不共享深度,做不出"转到灯泡背后被挡",需手工切 CSS 图层,脏且脆。

---

## 3. 作用域

**在内:** `landing-react` 的 Hero 视觉(灯泡/轨道/卫星/HUD/相机/交互)、Hero 配色与向下衔接、左右两栏配比、"WeDream AI"字标染色、点云与 logo 资源生成。

**不在内:** 下方三屏(Studio/Services/Join)的内容与卡片设计(仅统一其坐落的底色渐变)、顶栏功能、客服弹窗、后端、i18n 文案本身(沿用现有 key)。

---

## 4. 布局(两栏重配比)

- 保持两栏 Hero。**左文字区 ≈35%、右视觉区 ≈65%**(现约五五开)。
- 左栏文字**收窄 + 字号调小**(badge / h1 / 副标题 / 特性 / CTA 按比例缩),让灯泡+轨道更突出。
- **A1 让位动作:** 悬停锁定某模型时,`.hero-visual` 施加 `translateX(-~15%) scale(~.88)`(现有 `.card-open` 规则),右侧 HUD 卡滑入。**注意:此动作现由"点击"触发,本次改为"悬停锁定"触发**,并依赖 §5.4 的吸附锁定防抽搐。

---

## 5. 统一 3D 场景(`scene3d.ts`,新增)

单一 `<canvas>`,`WebGLRenderer` + `EffectComposer`(RenderPass + UnrealBloomPass + OutputPass),`PerspectiveCamera`,尺寸随 `.hero-right` 盒子响应(ResizeObserver)。StrictMode 双挂载用现有"模块级单例 + 引用计数 + 延迟卸载"化解(沿用 `index.tsx` 的 `boot/unboot`)。

### 5.1 灯泡(复用现状,外观不变)

沿用现 `bulb3d.ts` 的粒子点云 + 着色器(呼吸/噪声/鼠标斥力/twinkle)+ 辉光参数;数据仍取 `/lp-assets/dengpao_points.bin`(已有,40000 点)。**唯一变化:不再是定尺 940px 离屏画布,而是并入统一场景**,以便轨道/卫星与之做深度交互。视觉目标 = 和现在一致。保留 yun 的玻璃壳/灯座/辉光 sprite/灯丝(若现版已简化,以"现在的样子"为准)。

**鼠标斥力保留:** 光标靠近时灯泡粒子沿光标射线高斯散开(现有着色器 uniform `uMouseOrigin/uMouseDir/uRepelStrength`)。

### 5.2 两条 3D 轨道(倾斜管环)

用 `TubeGeometry(OrbitCurve, ...)` + yun 的 `orbitFragmentShader`(深度淡化 + 灯泡剪影遮罩 + 彗尾光迹 + 能量流)。每条轨道两层:细锐核心管 + 4× 宽的暗光晕。

| | 内圈 · 青色 | 外圈 · 深蓝 |
| --- | --- | --- |
| 颜色 | `#8fd8ff` 一带(青) | `#5f7cff` 一带(蓝) |
| 半径 | 较小 | 较大(调到 6 个图标间距舒服) |
| 承载 | **美国 4 个** | **中国 6 个** |
| 前方(近相机)运动方向 | **左 → 右**(绕灯泡右侧入后方,后方右→左返回;整体偏顺时针) | **右 → 左**(绕灯泡左侧入后方,后方左→右返回;与内圈相反) |
| 彗尾光流 | 跟随图标,前方左→右 | 跟随图标,前方右→左 |

- **进动摆(小幅振荡,非整圈翻转):** 内圈 ±4°、外圈**反相** ±6°,周期 ≈18s(落在用户区间:内 3–5°、外 4–7°、16–24s 内)。实现 = 每帧给轨道 group 的 tilt 叠加 `sin(2π·t/周期)·振幅`。
- **前后穿越(统一场景天然效果 + 着色器遮罩):** 图标到灯泡**后方**→ 缩小 / 变暗 / 降透明,且轨道后半弧被灯泡剪影遮罩淡出(`uBulbView`/`uMaskRadius`);到**前方**→ 放大 / 变亮 / 品牌光晕增强(即用户所说"阴影增强",在发光太空场景里做成**光晕/描边增强 + 略放大**,不用黑色投影)。
- **方向正负号:** three.js 里"前方左→右"取决于 speed 正负号与 tilt 朝向,**实现时对着 playwright 截图逐条校验、必要时翻号**;本规格以"肉眼可见方向"为准。

### 5.3 卫星模型(移植 yun 三层全息)

每个卫星 = 一个 `Group`,三层(后→前):
1. **品牌色粒子点云光晕** —— 从该模型 logo 生成的点云(见 §7),`PointsMaterial` 品牌色 + AdditiveBlending + twinkle(尺寸/透明度随时间微动)。纯氛围,永不作为点击/悬停命中目标(`raycast=()=>{}`)。
2. **微光背 + 细金属环** —— 品牌色 glow sprite(暗)+ `TorusGeometry` 细环 + 极淡玻璃盘。
3. **logo 图平面** —— **你项目自己的 logo**(见 §7),`MeshBasicMaterial{ map, toneMapped:false }`,每帧做**朝向相机的 billboard**(抵消父轨道 group 的倾斜:`local = parentQuat⁻¹ · cameraQuat`)。这是识别层。
- 远近淡化/缩放:`distFactor`(近 1.0 → 远 0.5),logo 远端仍可辨(下限抬高)。
- 品牌色取自现有 `MODELS` 表(§6),逐模型一色。

### 5.4 相机与交互

- **相机固定,不给拖拽**(移除/停用 OrbitControls)。理由:用户把前/后、左/右方向定死,能拖则方向失真。可含极轻微的场景自呼吸(可选,默认关,先做纯固定)。
- **悬停交互(碰到即触发,无需点击):**
  1. Raycaster 命中某卫星 → **锁定该模型**。
  2. 整个轨道系统**平滑缓停**:用累积相位时钟 `orbitPhase += dt · speedFactor`,`speedFactor` 在 1↔0 间缓动(悬停→0,移开→1);所有卫星角度、彗尾角、能量流**都读 `orbitPhase`**,故一起平滑冻结/恢复、**从停住位置续走不跳帧**。灯泡自身呼吸/twinkle 不受影响。
  3. 被选图标**放大变亮**;灯泡核心**染成该模型品牌色**(现有 `__bulb3d.setCoreColor` 逻辑并入场景)。
  4. 右侧 **HUD 卡淡入**、`.hero-visual` 按 **A1** 缩放让位。
  5. 字标染色(§7.1)。
- **吸附锁定(hysteresis)防抽搐:** 选中一旦锁定到某图标,即便画面缩小、图标随缓停漂移、光标暂落空处,**都不松手**;仅当光标**移到另一图标**或**明显离开视觉区**(留一点缓冲)才切换/关闭。这是"悬停缩小让位"不闪的关键。
- **点击:** 非主交互;先保留一个轻微的核心色脉冲(yun `triggerCoreSurge` 减配),或空操作。不做导航。
- **触屏兜底:** 无 hover → 改为轻触选中;或退化为无停轮播。移动端点云数量下调。

---

## 6. HUD(固定卡片 · 方案 A)

- 沿用现有 `#hud` DOM 卡片与**固定位置**(灯泡区旁)。悬停哪个模型就填哪个内容。
- 文案走**现有中文 i18n**(`MODELS` 表里的英文 key → `i18n.t()` 取译文,W5)。沿用现 `orbit.ts` 的 `MODELS`/`renderCard` 逻辑搬入 `scene3d.ts`,**不引入 yun 的英文硬编码**。
- 卡片左边框/圆点染成该模型品牌色(现有逻辑)。

---

## 7. 资源

**运行时(无需预生成任何新资源文件 —— 计划期发现的简化,取代原 .bin/PNG 烘焙管线):**
- 灯泡点云 `/lp-assets/dengpao_points.bin`(**已有,唯一预烘焙资源**)。
- 卫星 billboard 纹理:5 个已有 PNG(`qwen/doubao/kimi/glm_chatglm/minimax`,在 `/lp-assets/logos/`)直接加载;5 个内联 SVG(`openai/anthropic/gemini/xai/deepseek`,在 `orbit.ts` 的 `LOGOS`)**运行时** SVG→`CanvasTexture`。
- 卫星光晕点云:**运行时**在浏览器把上面的 billboard 纹理画到离屏 canvas、采其 alpha 像素生成(见 §7.2)。

**不需要** yun 那堆 80–90MB 源素材,**也不需要**预生成 PNG 或 `.bin` —— 全部客户端从现有资源即时生成,Mac dev 直接可见。

### 7.0 名单与品牌色(取自现有 `MODELS`)

| 环 | 模型 | key | 品牌色 | 现有 logo 形态 |
| --- | --- | --- | --- | --- |
| 内·青(美 4) | OpenAI | `openai` | `#10d075` | 内联 SVG → 栅格化 |
| | Claude | `anthropic` | `#d97757` | 内联 SVG → 栅格化 |
| | Gemini | `gemini` | `#9020f0` | 内联 SVG → 栅格化 |
| | Grok | `xai` | `#00a0ff` | 内联 SVG → 栅格化 |
| 外·蓝(中 6) | DeepSeek | `deepseek` | `#4fa3ff` | 内联 SVG → 栅格化 |
| | 通义千问 | `qwen` | `#6b6dff` | **已有 PNG** |
| | MiniMax | `minimax` | `#b987ff` | **已有 PNG** |
| | 豆包 | `doubao` | `#39c5ff` | **已有 PNG** |
| | Kimi | `kimi` | `#6c7cff` | **已有 PNG** |
| | 智谱 GLM | `glm_chatglm` | `#6f7bff` | **已有 PNG** |

> 现有 PNG 在 `web/default/public/lp-assets/logos/`;内联 SVG 在 `orbit.ts` 的 `LOGOS`。

### 7.1 字标染色 & Hero 配色

- **CSS 变量 `--wd-brand`**(仿 yun 的 `--glow-cyan`):默认 = 基准色;悬停锁定某模型时场景把它设为该模型品牌色,移开复位。
  - **Hero 大标题第一行 "WeDream AI"(`.hl1`)** 与 **左上角页眉 logo 文字(`.lp-logo span`)** 都 `color:var(--wd-brand)`,带 `transition:color .4s`,随选中平滑染色。
  - **Hero 大标题第二行 "让灵感不再受限"(`.hl2`)= 纯白**(静态,黑底上干净、与会变色的第一行形成对比)。
  - `.hl1` 默认色保持现状,若在黑底上发闷则微调(实现时定)。
- **Hero 纯黑 + 向下无缝衔接:**
  - 整页底色改为**竖直渐变**:顶部(Hero)近纯黑 `#01020a` → 往下渐融到下方三屏的深藏青(≈现 `#020610`/`#081226` 一带);渐变带跨过 Hero 与第一屏交界,**无硬线**。
  - 蓝紫**中心辉光收进灯泡背后**、不再铺满全页 → Hero 读作"黑"而非"藏青"。
  - **星尘/极光在 Hero 段调淡**保证黑;下方三屏维持现蓝调生气,卡片配色不动。

### 7.2 光晕点云 —— 运行时生成(取代预烘焙 `.bin`)

- 在 `scene3d.ts` init 时,对每个模型:把它的 billboard 纹理(§7 的 PNG 或 SVG→canvas)画到一个离屏 `<canvas>`(如 128²)→ `getImageData` 读 alpha → 在**不透明像素**中采样 ~1500–2000 点 → 归一化到 `[-0.5,0.5]` 的 x/y → z 加**小幅抖动**补一点厚度 → 得到 `Float32Array([x,y,z]×N)` 直接喂给该卫星的 `BufferGeometry`。
- 优点:零新资源文件、零构建脚本、Mac dev 即时可见、光晕永远与当前 logo 一致。
- 与 yun 差异:yun 的点云采自 logo 的 3D GLB(有真厚度),我们从 2D 纹理采(近似平面)。见 §9 取舍 1。
- 纯函数 `sampleAlphaToPoints(imageData, count) → Float32Array` 可单测(`bun test`)。

---

## 8. 涉及文件

**新增**
- `web/default/src/features/landing-react/scene3d.ts` — 统一 3D 场景(灯泡+轨道+卫星+交互+HUD 驱动),**替代 `orbit.ts` 并吸收 `bulb3d.ts`**。
- `scene3d-assets.ts` — 纹理/光晕生成纯函数(`svgToTexture`、`sampleAlphaToPoints` 等),便于 `bun test`。
- **无新资源文件**(billboard 纹理 + 光晕点云均运行时生成;见 §7)。参考源 `bulb-orbit/yun-reference/main.js`(移植底本,不入 web 构建)。

**修改**
- `index.tsx` — 挂单一场景画布;移除 `#orbits` SVG、`.chip` DOM、`#bulb`(PNG 兜底可留)、旧 `#bulb3d`;`.hl2` 纯白;字标绑 `--wd-brand`;`.card-open` 改由悬停锁定触发。
- `landing-css.ts` — 35/65 配比、左栏字号、A1 让位、`--wd-brand`、Hero 黑底竖直渐变、辉光收束、星尘/极光在 Hero 调淡。
- `sections-css.ts` — 仅把三屏坐落底色对齐到新渐变(若需)。

**移除/吸收**
- `orbit.ts`(SVG 轨道逻辑,由 `scene3d.ts` 取代)。
- `bulb3d.ts`(灯泡逻辑并入 `scene3d.ts`)。

---

## 9. 取舍与风险(已如实告知用户)

1. **光晕比 yun 略"扁"** —— 点云采自 2D logo 而非 3D 模型。缓解:相机固定不转 + 光晕本就是 logo 背后的暗层 + 加 z 抖动补厚度;基本不可察。
2. **自有 logo 不如 yun 鲜艳** —— yun 用满色徽标,我方有几个是单色符。这是"用你的 logo"的代价;个别想升级以后单换。
3. **把全窗口场景塞进 65% 盒子** —— 需调相机距离/fov 或轨道半径,让两环在盒内不裁切、6 个图标不挤。对着截图调。
4. **性能** —— 单场景约 40k 灯泡点 + 10×~2k 光晕点 + 2 管环 + bloom,量级同 yun,桌面无虞。加 `prefers-reduced-motion` 降级 + WebGL 失败回退 PNG(现有兜底)。
5. **StrictMode 双挂载** —— 沿用现有单例+引用计数。
6. **移动端** —— 无 hover;触选或轮播兜底,点数下调(§5.4)。

---

## 10. 验收判据(Definition of Done)

- [ ] 灯泡外观与现在一致,且现在能看到卫星从其**前面经过压住、后面经过被挡**。
- [ ] 内圈青(美 4)前方**左→右**、外圈蓝(中 6)前方**右→左**,彗尾光流方向跟随图标。
- [ ] 两环有 ±4°/±6°、~18s 的反相小幅进动摆。
- [ ] 悬停任一图标 → 轨道**平滑缓停**(无顿挫)、移开**平滑恢复**且不跳帧;该图标放大变亮、灯泡核心染品牌色、HUD 淡入、画面 A1 让位;**吸附锁定不抽搐**。
- [ ] HUD 文案为中文(i18n)。
- [ ] 选中时"WeDream AI"(Hero 标题 + 页眉)平滑染成品牌色;"让灵感不再受限"为纯白。
- [ ] Hero 读作纯黑、向下渐融进藏青无硬边;辉光只在灯泡后。
- [ ] 左 35% / 右 65%,文字明显收小、视觉更突出。
- [ ] 服务器构建通过;playwright 截图逐条核对上述项;`prefers-reduced-motion` 与 WebGL 失败均有兜底。

---

## 11. 遗留待定(实现时定,不阻塞)

- 具体环半径 / 相机距离 / fov 数值(对截图调)。
- `.hl1` 默认色是否在黑底上微调。
- 点击的最终行为(轻微脉冲 vs 空操作)。
