# Claude 前端实现说明：灯泡慢慢照亮的 HTML 动效

## 目标效果

当前页面是一个 HTML 前端界面。请在不改变核心布局的前提下，把 hero 区做成“灯泡先亮起，然后光慢慢照射到轨道、logo 和标题，其它区域保持黑”的动态效果。

关键约束：

- 轨道数量只能保持 2 条，不能新增第三条轨道。
- 不要重排标题文案。
- 不要把整个页面做成大面积蓝色曝光，整体要偏黑、克制、有电影感。
- 灯泡是唯一光源，所有可见内容都应像是被灯泡照亮。
- 默认首屏应该是黑场中逐渐亮起，而不是一开始全部可见。

## 可用参考文件

这些文件是效果参考，不一定都要作为页面素材使用：

- `/Users/cc/Documents/Codex/2026-07-03/xu-ya/outputs/bulb-light-previews/05_dark_frame_1_bulb_only.png`
  - 开场参考：只亮灯泡附近。
- `/Users/cc/Documents/Codex/2026-07-03/xu-ya/outputs/bulb-light-previews/07_dark_frame_3_orbit_sweep.png`
  - 中段参考：光沿两条轨道扫出去。
- `/Users/cc/Documents/Codex/2026-07-03/xu-ya/outputs/bulb-light-previews/08_dark_frame_4_controlled_final.png`
  - 最终态参考：界面可读，但四周仍然接近黑色。
- `/Users/cc/Documents/Codex/2026-07-03/xu-ya/outputs/bulb-light-previews/bulb-light-slow-illumination.gif`
  - 动画节奏参考。

可用素材：

- `/Users/cc/Documents/Codex/2026-07-03/xu-ya/outputs/lightbulb_cutout.png`
  - 透明背景灯泡。如果当前 HTML 的灯泡不好控制，可以用这个替换中心灯泡图片。
- `/Users/cc/Documents/Codex/2026-07-03/xu-ya/outputs/ai-logo-pack/logo-only/`
  - 透明 logo，适合放进现有圆形节点。
- `/Users/cc/Documents/Codex/2026-07-03/xu-ya/outputs/ai-logo-pack/orbit-badges/`
  - 已经带圆形底盘和发光效果的 logo 节点。

## 推荐动画阶段

建议做成 5.5s 左右的首屏入场动画：

1. `0s - 0.6s`：页面几乎全黑，只能看到灯泡灯丝微弱闪烁。
2. `0.6s - 1.8s`：灯泡玻璃边缘和底座逐渐亮起，中心出现小范围蓝色光晕。
3. `1.8s - 3.2s`：光沿两条轨道向左右扩散，轨道线和附近 logo 被扫亮。
4. `3.2s - 4.6s`：标题区域被轻微照亮，但仍然保持背景黑。
5. `4.6s - 5.5s`：进入最终态，灯泡、两条轨道、logo 和标题可读，页面边缘保持黑色。

## 推荐 DOM 层级

如果当前 HTML 已有 hero 结构，请尽量按现有结构改，不要大重构。建议最终结构类似：

```html
<section class="hero hero-lighting">
  <div class="hero-bg"></div>
  <div class="hero-grid"></div>

  <div class="hero-copy">
    <!-- 保留原有 badge、标题、副标题 -->
  </div>

  <div class="orbit-stage">
    <div class="orbit orbit-primary"></div>
    <div class="orbit orbit-secondary"></div>
    <!-- 保留或替换现有 logo nodes -->
    <img class="bulb" src="./assets/lightbulb_cutout.png" alt="" />
  </div>

  <!-- 新增光照层 -->
  <div class="lighting lighting-core"></div>
  <div class="lighting lighting-orbit-sweep"></div>
  <div class="lighting lighting-title"></div>
  <div class="darkness-mask"></div>
</section>
```

如果当前 HTML 不是这个结构，也可以只新增 `.lighting-*` 和 `.darkness-mask` 这些覆盖层。

## CSS 实现建议

把 hero 做成黑色底，所有光效用伪元素/覆盖层生成，不要直接把背景改亮。

```css
.hero-lighting {
  position: relative;
  overflow: hidden;
  min-height: 100vh;
  background:
    radial-gradient(circle at 50% 58%, rgba(10, 42, 70, 0.22), transparent 34%),
    #02050b;
  isolation: isolate;
}

.hero-lighting .hero-copy,
.hero-lighting .orbit-stage {
  position: relative;
  z-index: 4;
}

.hero-lighting .bulb {
  position: absolute;
  left: 50%;
  top: 56%;
  width: clamp(210px, 18vw, 330px);
  transform: translate(-50%, -50%);
  z-index: 5;
  filter:
    drop-shadow(0 0 12px rgba(54, 213, 255, 0.9))
    drop-shadow(0 0 42px rgba(21, 128, 255, 0.45));
  animation: bulbWake 5.5s ease-out both;
}

.lighting {
  pointer-events: none;
  position: absolute;
  inset: 0;
  z-index: 3;
  mix-blend-mode: screen;
}

.lighting-core {
  background:
    radial-gradient(ellipse 18% 22% at 50% 58%,
      rgba(175, 252, 255, 0.72) 0%,
      rgba(52, 213, 255, 0.34) 22%,
      rgba(28, 112, 210, 0.12) 48%,
      transparent 72%);
  animation: coreGlow 5.5s ease-out both;
}

.lighting-orbit-sweep {
  background:
    radial-gradient(ellipse 42% 9% at 50% 63%,
      rgba(72, 212, 255, 0.34) 0%,
      rgba(28, 137, 220, 0.18) 42%,
      transparent 76%),
    linear-gradient(90deg,
      transparent 0%,
      rgba(58, 204, 255, 0.03) 24%,
      rgba(96, 226, 255, 0.24) 48%,
      rgba(58, 204, 255, 0.03) 72%,
      transparent 100%);
  opacity: 0;
  transform: scaleX(0.14);
  transform-origin: 50% 60%;
  filter: blur(10px);
  animation: orbitSweep 5.5s ease-out both;
}

.lighting-title {
  background: radial-gradient(ellipse 24% 15% at 50% 22%,
    rgba(58, 190, 255, 0.16),
    transparent 72%);
  opacity: 0;
  animation: titleRevealLight 5.5s ease-out both;
}

.darkness-mask {
  pointer-events: none;
  position: absolute;
  inset: 0;
  z-index: 6;
  background: #000;
  opacity: 0.86;
  mix-blend-mode: multiply;
  mask-image:
    radial-gradient(ellipse 19% 22% at 50% 58%, transparent 0%, transparent 42%, black 76%),
    radial-gradient(ellipse 52% 28% at 50% 61%, transparent 0%, transparent 22%, black 72%);
  mask-composite: intersect;
  animation: darknessOpen 5.5s ease-out both;
}

@keyframes bulbWake {
  0% {
    opacity: 0.22;
    filter: drop-shadow(0 0 2px rgba(54, 213, 255, 0.35));
  }
  18% {
    opacity: 0.82;
    filter:
      drop-shadow(0 0 8px rgba(54, 213, 255, 0.7))
      drop-shadow(0 0 22px rgba(21, 128, 255, 0.3));
  }
  100% {
    opacity: 1;
    filter:
      drop-shadow(0 0 12px rgba(54, 213, 255, 0.88))
      drop-shadow(0 0 42px rgba(21, 128, 255, 0.42));
  }
}

@keyframes coreGlow {
  0% { opacity: 0; transform: scale(0.34); }
  22% { opacity: 0.82; transform: scale(0.78); }
  100% { opacity: 1; transform: scale(1); }
}

@keyframes orbitSweep {
  0%, 28% { opacity: 0; transform: scaleX(0.12); }
  58% { opacity: 0.9; transform: scaleX(0.86); }
  100% { opacity: 0.46; transform: scaleX(1); }
}

@keyframes titleRevealLight {
  0%, 48% { opacity: 0; }
  76% { opacity: 0.62; }
  100% { opacity: 0.34; }
}

@keyframes darknessOpen {
  0% { opacity: 0.96; }
  28% { opacity: 0.88; }
  62% { opacity: 0.62; }
  100% { opacity: 0.34; }
}
```

## 两条轨道的处理方式

不要新增轨道。只改现有两条轨道的可见性和光照状态：

```css
.orbit {
  opacity: 0.18;
  filter: drop-shadow(0 0 6px rgba(74, 207, 255, 0.18));
  animation: orbitReveal 5.5s ease-out both;
}

.orbit-primary {
  opacity: 0.22;
  stroke-width: 1.5px;
}

.orbit-secondary {
  opacity: 0.12;
  stroke-width: 1px;
}

@keyframes orbitReveal {
  0%, 32% {
    opacity: 0.04;
    filter: drop-shadow(0 0 2px rgba(74, 207, 255, 0.08));
  }
  62% {
    opacity: 0.72;
    filter: drop-shadow(0 0 12px rgba(74, 207, 255, 0.48));
  }
  100% {
    opacity: 0.42;
    filter: drop-shadow(0 0 8px rgba(74, 207, 255, 0.28));
  }
}
```

如果轨道是 SVG path，优先用 `stroke-dasharray` 和 `stroke-dashoffset` 做扫光：

```css
.orbit-path {
  stroke-dasharray: 120 760;
  stroke-dashoffset: 520;
  animation: orbitDashSweep 5.5s ease-out both;
}

@keyframes orbitDashSweep {
  0%, 30% { stroke-dashoffset: 760; opacity: 0.08; }
  60% { stroke-dashoffset: 180; opacity: 0.78; }
  100% { stroke-dashoffset: 0; opacity: 0.42; }
}
```

## logo 节点处理

logo 不要一开始全部亮。每个节点按距离灯泡或轨道顺序延迟出现：

```css
.model-node {
  opacity: 0.08;
  filter: brightness(0.45) drop-shadow(0 0 0 rgba(72, 212, 255, 0));
  animation: nodeLit 5.5s ease-out both;
  animation-delay: var(--node-delay, 0ms);
}

@keyframes nodeLit {
  0%, 34% {
    opacity: 0.06;
    filter: brightness(0.38);
  }
  58% {
    opacity: 1;
    filter:
      brightness(1.2)
      drop-shadow(0 0 14px rgba(72, 212, 255, 0.52));
  }
  100% {
    opacity: 0.82;
    filter:
      brightness(0.92)
      drop-shadow(0 0 8px rgba(72, 212, 255, 0.28));
  }
}
```

HTML 示例：

```html
<button class="model-node" style="--node-delay: 1600ms">
  <img src="./assets/logo-only/kimi.png" alt="Kimi" />
</button>
```

建议节点延迟：

- 靠近灯泡的 logo：`1200ms - 1800ms`
- 中间区域 logo：`1800ms - 2600ms`
- 最外侧 logo：`2600ms - 3600ms`

## JS 可选控制

如果希望滚动到 hero 时再播放，而不是页面加载立刻播放，可以加：

```html
<script>
  const hero = document.querySelector('.hero-lighting');

  const observer = new IntersectionObserver(([entry]) => {
    if (entry.isIntersecting) {
      hero.classList.add('is-lit');
      observer.disconnect();
    }
  }, { threshold: 0.45 });

  observer.observe(hero);
</script>
```

然后把动画默认暂停：

```css
.hero-lighting:not(.is-lit) *,
.hero-lighting:not(.is-lit)::before,
.hero-lighting:not(.is-lit)::after {
  animation-play-state: paused;
}
```

## 最重要的审美要求

- 最终画面不是“亮蓝色科技背景”，而是“黑暗里被灯泡照出来的科技界面”。
- 灯泡光要有范围，范围外必须明显变黑。
- 标题可以被照亮，但不能亮到像白屏。
- 轨道只能两条，丰富度来自光线、扫光、粒子和节点延迟，不来自新增轨道。
- 如果某个实现让灯泡变成一大块白色/青色椭圆，说明曝光过度，需要降低 glow 的 opacity 和 blur 范围。
