# bulb-orbit 粒子灯泡替换 · 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `wei500L/yun` 的 WebGL 粒子点云灯泡（含全部动效）替换进 `bulb-orbit/index.html`，替掉 PNG 灯泡；页面其余一切（2D 轨道、24 芯片、中文文案、两段式交互）不变。

**Architecture:** 全屏之内、以灯泡为中心的**方形透明 WebGL 画布**插在原 `#halo`/`#bulb` 层位（`pointer-events:none`，事件全部从 `window` 监听换算）；three r160 + 后期模块 + 点云 base64 由拼装脚本注入 `index.html` 的标记区，成品仍是**双击即开的单文件**；WebGL 失败自动回退原 PNG。设计定稿见 `docs/superpowers/specs/2026-07-07-bulb-orbit-particle-bulb-design.md`。

**Tech Stack:** three@0.160.0（vendored 拼接，无运行时依赖）、原生 JS/CSS、python3 标准库（拼装脚本）、无头 Chrome + playwright-cli（验证）。

## Global Constraints

- three 固定 `0.160.0`（与 yun 的 `^0.160.0` 对齐），不得静默升级。
- 成品页面**运行时零网络请求**：断网 `file://` 双击可用；禁止任何 CDN 运行时引用。
- 不新增/不修改任何界面文字（W5：现有中文文案与「点 击 点 亮」提示原样保留）。
- 无头 Chrome / playwright 每轮验证后**立即关闭**，`ps aux | grep -iE 'headless|playwright' | grep -v grep` 复核零残留（W6）。
- git 提交只用**显式路径**（工作区有其他未提交改动）；严禁 `git add .` / `git add bulb-orbit/`。
- 不改 `ignite()`/两段式状态机语义与 2D 点亮时间轴数值；`--cy:57%`、芯片 z-index 2..10 体系不动。
- 拼装脚本仅用 python3 标准库；本任务只在 Mac 本地进行（纯前端素材，不涉及服务器部署）。
- yun 克隆位于 `/private/tmp/claude-501/-Users-cc-newapi628/c510bd99-9f56-46e0-a6a7-73a693f4abfe/scratchpad/yun`（若丢失，重取：`git clone --depth=1 https://github.com/wei500L/yun <目标>`，只需其中 `public/models/dengpao_points.bin`）。
- 参考基准图：`bulb-orbit/阶段2-点击点亮-全景与文字.png`（改前旧版）、scratchpad `yun/hero-logo-satellite-final.png`（形象目标）。

---

### Task 1: 素材入库 + vendor 拼装脚本 + index.html 注入区

**Files:**
- Create: `bulb-orbit/tools/build-single.py`
- Create: `bulb-orbit/tools/vendor-src/`（11 个下载文件）
- Create: `bulb-orbit/assets/dengpao_points.bin`（从 yun 克隆拷贝，938KB）
- Modify: `bulb-orbit/index.html`（`</body>` 前新增带标记区的空模块脚本）

**Interfaces:**
- Produces（后续任务依赖）：模块脚本内的顶层绑定——three 全部公开名（`Scene`、`PerspectiveCamera`、`WebGLRenderer`、`ShaderMaterial`、`EffectComposer`、`RenderPass`、`UnrealBloomPass`、`OutputPass`、`ShaderPass` 等均为 `const`）；`DENGPAO_B64_CHUNKS: string[]`；探针 `window.__bulb3dVendorOK: boolean`、`window.__bulbDataB64Len: number`（应为 1280000）。
- 标记区协议：`/* ===== BULB3D-VENDOR-BEGIN … ===== */` / `BULB3D-VENDOR-END`、`BULB3D-DATA-BEGIN` / `BULB3D-DATA-END`，脚本幂等替换标记之间内容。

- [ ] **Step 1: 建目录并拷点云**

```bash
mkdir -p /Users/cc/newapi628/bulb-orbit/tools/vendor-src /Users/cc/newapi628/bulb-orbit/assets
cp "/private/tmp/claude-501/-Users-cc-newapi628/c510bd99-9f56-46e0-a6a7-73a693f4abfe/scratchpad/yun/public/models/dengpao_points.bin" \
   /Users/cc/newapi628/bulb-orbit/assets/dengpao_points.bin
ls -l /Users/cc/newapi628/bulb-orbit/assets/dengpao_points.bin   # 期望 960000 字节
```

- [ ] **Step 2: 下载 three@0.160.0 与后期模块（11 个文件）**

```bash
cd /Users/cc/newapi628/bulb-orbit/tools/vendor-src
BASE=https://cdn.jsdelivr.net/npm/three@0.160.0
curl -fsSL -o three.module.min.js "$BASE/build/three.module.min.js"
for f in Pass MaskPass ShaderPass RenderPass EffectComposer UnrealBloomPass OutputPass; do
  curl -fsSL -o "$f.js" "$BASE/examples/jsm/postprocessing/$f.js"; done
for f in CopyShader LuminosityHighPassShader OutputShader; do
  curl -fsSL -o "$f.js" "$BASE/examples/jsm/shaders/$f.js"; done
ls -la   # three.module.min.js 约 650-700KB，其余每个 1-20KB
```

jsdelivr 不通时把 `BASE` 换成 `https://unpkg.com/three@0.160.0`。

- [ ] **Step 3: 在 index.html 加入空标记区模块脚本**

在 `bulb-orbit/index.html` 的 `</body>` 之前（现第 482 行 `</script>` 之后）插入：

```html
<script type="module">
/* ===== BULB3D-VENDOR-BEGIN（tools/build-single.py 生成：three@0.160.0 + 后期通道拼接；勿手改） ===== */
/* ===== BULB3D-VENDOR-END ===== */
/* ===== BULB3D-DATA-BEGIN（tools/build-single.py 生成：assets/dengpao_points.bin 的 base64；勿手改） ===== */
/* ===== BULB3D-DATA-END ===== */
/* ===== BULB3D-CODE：3D 灯泡装配（手写区，Task 2/3 实现） ===== */
</script>
```

- [ ] **Step 4: 写拼装脚本**

创建 `bulb-orbit/tools/build-single.py`：

```python
#!/usr/bin/env python3
"""bulb-orbit 单文件拼装：把 three r160(+后期) 与 dengpao 点云 base64 注入 index.html 标记区。
用法：python3 tools/build-single.py   （在 bulb-orbit/ 下运行；幂等，可重复执行）
只在换 three 版本或换点云数据时需要重跑；日常改页面不用。仅 python3 标准库。"""
import base64, pathlib, re, sys

ROOT = pathlib.Path(__file__).resolve().parent.parent          # bulb-orbit/
SRC  = ROOT / 'tools' / 'vendor-src'
HTML = ROOT / 'index.html'
BIN  = ROOT / 'assets' / 'dengpao_points.bin'

# 依赖序：先 shader 对象，再 Pass 基类，再各通道（后者引用前者的顶层名）
ADDONS = ['CopyShader.js', 'LuminosityHighPassShader.js', 'OutputShader.js', 'Pass.js',
          'MaskPass.js', 'ShaderPass.js', 'RenderPass.js', 'EffectComposer.js',
          'UnrealBloomPass.js', 'OutputPass.js']

def three_to_consts(src: str) -> str:
    """three.module.min.js 内部名是压缩名，公开名只存在于末尾 export{A as B,...}。
    把它改写成 const B=A,... 使后续拼接的模块能用公开名。"""
    src = re.sub(r'//# sourceMappingURL=\S+\s*$', '', src)
    last = None
    for last in re.finditer(r'export\{([^}]*)\};?', src):
        pass
    if last is None:
        sys.exit('three.module.min.js: 未找到 export{...}，构建终止')
    body = src[:last.start()] + src[last.end():]
    pairs = []
    for item in last.group(1).split(','):
        item = item.strip()
        if not item:
            continue
        if ' as ' in item:
            a, b = [x.strip() for x in item.split(' as ')]
            if a != b:
                pairs.append(f'{b}={a}')
        # 无别名（a 即公开名）：顶层已存在同名声明，无需重绑
    return body + '\nconst ' + ','.join(pairs) + ';\n'

def strip_module(src: str) -> str:
    """examples/jsm 源码：去 import（依赖名已在拼接作用域内）、去 export 关键字。"""
    src = re.sub(r'import\s[\s\S]*?from\s*[\'"][^\'"]+[\'"];?', '', src)
    src = re.sub(r'export\s*\{[^}]*\};?', '', src)
    return (src.replace('export class ', 'class ')
               .replace('export const ', 'const ')
               .replace('export function ', 'function '))

def replace_block(html: str, tag: str, content: str) -> str:
    begin = f'/* ===== {tag}-BEGIN'
    end   = f'/* ===== {tag}-END'
    i = html.index(begin); i = html.index('*/', i) + 2
    j = html.index(end)
    return html[:i] + '\n' + content + '\n' + html[j:]

vendor = [three_to_consts((SRC / 'three.module.min.js').read_text())]
for name in ADDONS:
    vendor.append(f'\n/* ---- {name} ---- */\n' + strip_module((SRC / name).read_text()))
vendor.append("\nwindow.__bulb3dVendorOK = (typeof Scene==='function' && typeof EffectComposer==='function'"
              " && typeof UnrealBloomPass==='function' && typeof OutputPass==='function'"
              " && typeof ShaderPass==='function' && typeof RenderPass==='function');\n")
vendor_js = ''.join(vendor)
if '</script' in vendor_js.lower():
    sys.exit('vendor 代码含 </script>，会截断 HTML——需先处理再注入')

b64 = base64.b64encode(BIN.read_bytes()).decode()
chunks = ',\n'.join(f'"{b64[i:i+4000]}"' for i in range(0, len(b64), 4000))
data_js = (f'const DENGPAO_B64_CHUNKS=[\n{chunks}\n];\n'
           f'window.__bulbDataB64Len={len(b64)};\n')

html = HTML.read_text()
html = replace_block(html, 'BULB3D-VENDOR', vendor_js)
html = replace_block(html, 'BULB3D-DATA', data_js)
HTML.write_text(html)
print(f'ok: vendor={len(vendor_js)//1024}KB data={len(data_js)//1024}KB index.html={len(html)//1024}KB')
```

- [ ] **Step 5: 运行拼装并核对体量**

```bash
cd /Users/cc/newapi628/bulb-orbit && python3 tools/build-single.py
ls -lh index.html   # 期望 ~2.0-2.2MB；脚本输出 ok: vendor=~740KB data=~1290KB
```

- [ ] **Step 6: 探针验证（playwright-cli，用完即关）**

用 playwright-cli 技能打开 `file:///Users/cc/newapi628/bulb-orbit/index.html`，evaluate 两个表达式：

- `window.__bulb3dVendorOK` → 期望 `true`（three + 全部后期类在作用域内）
- `window.__bulbDataB64Len` → 期望 `1280000`（960000 字节 ÷3×4，无填充）

随后关闭浏览器并复核：

```bash
ps aux | grep -iE 'headless|playwright' | grep -v grep   # 期望无输出
```

若 `__bulb3dVendorOK` 为 false/undefined：读 playwright 控制台报错定位（常见：export 正则误伤→检查 vendor-src 对应文件被替换的位置）。

- [ ] **Step 7: 提交（含首次素材入库）**

```bash
cd /Users/cc/newapi628
git add bulb-orbit/tools bulb-orbit/assets/dengpao_points.bin bulb-orbit/index.html \
        bulb-orbit/bulb.png bulb-orbit/logos bulb-orbit/交互修改文档.md
git commit -m "feat(bulb-orbit): vendor 拼装管线（three@0.160.0+后期+点云 base64 注入单文件）"
```

---

### Task 2: 3D 灯泡装配（视觉主体 + 回退，无鼠标交互）

**Files:**
- Modify: `bulb-orbit/index.html`（CSS 三处小改 + 1 行 HTML + BULB3D-CODE 手写区全部代码）

**Interfaces:**
- Consumes：Task 1 的顶层 three 绑定与 `DENGPAO_B64_CHUNKS`。
- Produces（Task 3 依赖，全部在模块作用域）：`canvas3d`（画布元素）、`camera`、`particleMaterial`（uniforms 含 `uRepelStrength`）、`glowSprite`、`filament`、`pointLight`、`root`；状态变量 `let surgeT0`（秒，-1e9=未触发）、`const mouseNDC = new Vector2(0,0)`、`let pointerInCanvas = false`、`const cursorRayDir = new Vector3(0,0,-1)`、`const PRM`（reduced-motion 布尔）；`window.__bulb3dActive === true`（初始化成功后置位）。渲染循环每帧读取 `mouseNDC`/`pointerInCanvas`/`surgeT0`——Task 3 只写这些状态，不改渲染代码。

- [ ] **Step 1: CSS——新增画布定位与显隐规则**

`index.html` 三处编辑：

(a) 第 94 行 `:root { --chip:56px; --cy:57%; }` 改为（1.7 = 画布相对灯泡的留白系数，两处 min() 的分界点相同，灯泡恒占画布 1/1.7）：

```css
  :root { --chip:56px; --cy:57%; --b3d:min(73.1vh, 98.6vw); }   /* --cy must match CY in the JS layout; --b3d = 1.7×灯泡高 */
```

(b) 紧跟原 `#bulb { ... }` 规则（第 91 行 `}` 之后）插入：

```css
  #bulb3d {   /* 3D 粒子灯泡画布：占原 #halo/#bulb 层位，方形、居中于 --cy，事件全穿透 */
    position:absolute; left:50%; top:var(--cy); z-index:6; pointer-events:none;
    width:var(--b3d); height:var(--b3d);
    margin:calc(var(--b3d) / -2) 0 0 calc(var(--b3d) / -2);
    display:none;
  }
  body.webgl3d #bulb3d { display:block; }
  body.webgl3d #halo, body.webgl3d #bulb { display:none; }   /* WebGL 就绪后 PNG 灯泡退场（仍作回退保留） */
```

(c) reduced-motion 媒体查询块不动（PNG 回退态仍按原规则降级；3D 侧由 JS 的 `PRM` 处理）。

- [ ] **Step 2: HTML——插入画布元素**

第 241 行 `<img id="bulb" ...>` 之后插入：

```html
  <canvas id="bulb3d"></canvas>
```

- [ ] **Step 3: BULB3D-CODE 手写区写入装配代码**

替换 `/* ===== BULB3D-CODE：3D 灯泡装配（手写区，Task 2/3 实现） ===== */` 该行之后（至 `</script>` 前）为以下完整代码。shader 与全部数值逐字来自 yun `src/main.js`（排斥/呼吸/闪烁/色渐变/玻璃罩/灯座/灯丝/辉光/泛光），差异仅：无雾、无 OrbitControls、无 GSAP、无卫星，新增亮度转 alpha 合成与包围盒自动取景。

```js
/* ===== BULB3D-CODE：3D 灯泡装配（手写区；源自 wei500L/yun src/main.js，参数原样） ===== */
const PRM = matchMedia('(prefers-reduced-motion: reduce)').matches;
const FROZEN_T = 12.0;                               /* reduced-motion 的静态时刻（秒） */
const canvas3d = document.getElementById('bulb3d');

const bulbVertexShader = `
  uniform float uTime;
  uniform float uSize;
  uniform vec3 uMouseOrigin;
  uniform vec3 uMouseDir;
  uniform float uRepelStrength;
  uniform float uRepelRadius;
  uniform float uRepelPower;
  attribute vec3 aNormal;
  attribute float aRandom;
  varying vec3 vColor;
  void main() {
    float breathingOffset = sin(uTime * 1.1 + aRandom * 6.28318) * 0.006;
    vec3 noiseOffset = vec3(
      sin(uTime * 0.5 + aRandom * 25.0),
      cos(uTime * 0.7 + aRandom * 30.0),
      sin(uTime * 0.9 + aRandom * 35.0)
    ) * 0.002;
    vec3 finalPos = position + aNormal * breathingOffset + noiseOffset;
    float breath = 1.0 + sin(uTime * 0.7) * 0.012;
    finalPos *= breath;
    vec3 worldPos = (modelMatrix * vec4(finalPos, 1.0)).xyz;
    vec3 toParticle = worldPos - uMouseOrigin;
    vec3 closest = uMouseOrigin + uMouseDir * dot(toParticle, uMouseDir);
    vec3 away = worldPos - closest;
    float rayDist = length(away);
    float force = uRepelStrength * exp(-(rayDist * rayDist) / (uRepelRadius * uRepelRadius));
    vec3 pushDir = rayDist > 0.0001 ? away / rayDist : aNormal;
    worldPos += pushDir * force * uRepelPower * (0.75 + 0.5 * aRandom);
    float radialDist = length(position.xz);
    vec3 coreColor = vec3(0.85, 0.94, 1.0);
    vec3 edgeColor = vec3(0.0, 0.65, 1.0);
    vColor = mix(coreColor, edgeColor, smoothstep(0.06, 0.35, radialDist));
    vColor = mix(vColor, vec3(0.75, 0.95, 1.0), force * 0.6);
    vec4 mvPosition = viewMatrix * vec4(worldPos, 1.0);
    gl_Position = projectionMatrix * mvPosition;
    float twinkle = 0.7 + 0.3 * sin(uTime * (1.6 + aRandom * 2.2) + aRandom * 6.28318);
    gl_PointSize = uSize * (300.0 / -mvPosition.z) * twinkle;
  }
`;
const bulbFragmentShader = `
  varying vec3 vColor;
  void main() {
    float dist = length(gl_PointCoord - vec2(0.5));
    if (dist > 0.5) discard;
    float alpha = smoothstep(0.5, 0.06, dist);
    float core = smoothstep(0.12, 0.0, dist) * 0.6;
    vec3 finalColor = mix(vColor, vec3(1.0), core * 0.4);
    gl_FragColor = vec4(finalColor, (alpha * 0.45 + core * 0.15) * 0.75);
  }
`;
/* 透明画布 × Bloom 的合成：末端把 alpha 置为 max(r,g,b)。深色页面底下与加法发光等效，
   且保证 rgb ≤ alpha（预乘合成合法），泛光延伸区不再因 alpha=0 而丢失。 */
const AlphaFromLumaShader = {
  uniforms: { tDiffuse: { value: null } },
  vertexShader: `varying vec2 vUv;
    void main() { vUv = uv; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
  fragmentShader: `uniform sampler2D tDiffuse; varying vec2 vUv;
    void main() {
      vec4 c = texture2D(tDiffuse, vUv);
      gl_FragColor = vec4(c.rgb, clamp(max(c.r, max(c.g, c.b)), 0.0, 1.0));
    }`
};

function decodeDengpao() {
  const b64 = DENGPAO_B64_CHUNKS.join('');
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  const f = new Float32Array(bytes.buffer);
  if (f.length !== 240000) throw new Error('dengpao 数据长度异常: ' + f.length);
  return f;                                          /* 40000 × (pos.xyz + normal.xyz) */
}

function createGlowTexture(colorHex) {               /* yun createGlowSpriteTexture 原样 */
  const c = document.createElement('canvas');
  c.width = 64; c.height = 64;
  const ctx = c.getContext('2d');
  const grad = ctx.createRadialGradient(32, 32, 0, 32, 32, 32);
  grad.addColorStop(0, colorHex);
  grad.addColorStop(0.2, colorHex);
  grad.addColorStop(1, 'rgba(0, 0, 0, 0)');
  ctx.fillStyle = grad;
  ctx.fillRect(0, 0, 64, 64);
  return new CanvasTexture(c);
}

let renderer, scene, camera, composer, root, particleMaterial, glowSprite, filament, pointLight;
let surgeT0 = -1e9;                                  /* 核心喷发起点（秒）；Task 3 写入 */
const mouseNDC = new Vector2(0, 0);                  /* 画布局部 NDC；Task 3 写入 */
let pointerInCanvas = false;                         /* Task 3 写入 */
const cursorRayDir = new Vector3(0, 0, -1);

function initBulb3D() {
  if (/nowebgl/i.test(location.search)) throw new Error('forced no-webgl (test hook)');
  const f = decodeDengpao();

  renderer = new WebGLRenderer({ canvas: canvas3d, antialias: true, alpha: true });
  renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
  renderer.toneMapping = ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.0;
  renderer.setClearColor(0x000000, 0);

  scene = new Scene();                               /* 无雾无背景：透明画布 */
  camera = new PerspectiveCamera(45, 1, 0.1, 100);   /* 方形画布，aspect 恒 1 */
  camera.position.set(0, 0, 4.6);

  scene.add(new AmbientLight('#040d20', 1.5));
  pointLight = new PointLight('#00f0ff', 1.5, 8);
  pointLight.position.set(0, 0.14, 1.2);
  scene.add(pointLight);

  root = new Group();                                /* 点云+装配同组：整体自旋/重定中心 */
  scene.add(root);

  const positions = new Float32Array(40000 * 3);
  const normals = new Float32Array(40000 * 3);
  const randoms = new Float32Array(40000);
  let minY = Infinity, maxY = -Infinity;
  for (let i = 0; i < 40000; i++) {
    const y = f[i * 6 + 1];
    positions[i * 3] = f[i * 6];  positions[i * 3 + 1] = y;  positions[i * 3 + 2] = f[i * 6 + 2];
    normals[i * 3] = f[i * 6 + 3]; normals[i * 3 + 1] = f[i * 6 + 4]; normals[i * 3 + 2] = f[i * 6 + 5];
    randoms[i] = Math.random();
    if (y < minY) minY = y;
    if (y > maxY) maxY = y;
  }
  const geo = new BufferGeometry();
  geo.setAttribute('position', new BufferAttribute(positions, 3));
  geo.setAttribute('aNormal', new BufferAttribute(normals, 3));
  geo.setAttribute('aRandom', new BufferAttribute(randoms, 1));
  particleMaterial = new ShaderMaterial({
    vertexShader: bulbVertexShader,
    fragmentShader: bulbFragmentShader,
    uniforms: {
      uTime: { value: 0.0 },
      uSize: { value: 0.045 },
      uMouseOrigin: { value: new Vector3(0, 0, 99) },
      uMouseDir: { value: new Vector3(0, 0, -1) },
      uRepelStrength: { value: 0.0 },
      uRepelRadius: { value: 0.26 },
      uRepelPower: { value: 0.16 }
    },
    transparent: true,
    depthWrite: false,
    blending: AdditiveBlending
  });
  root.add(new Points(geo, particleMaterial));

  /* 玻璃罩/灯座/灯丝/辉光：yun setupGlassCore 原参数 */
  const glassMat = new MeshPhongMaterial({
    color: 0x8be5ff, transparent: true, opacity: 0.05, shininess: 120,
    specular: 0xffffff, side: DoubleSide, depthWrite: false
  });
  const dome = new Mesh(new SphereGeometry(0.36, 40, 40), glassMat);
  dome.position.y = 0.14;
  root.add(dome);
  const neck = new Mesh(new CylinderGeometry(0.36, 0.20, 0.32, 32, 1, true), glassMat);
  neck.position.y = -0.16;
  root.add(neck);
  const cap = new Mesh(
    new CylinderGeometry(0.18, 0.18, 0.18, 32),
    new MeshStandardMaterial({ color: 0x1a2b42, metalness: 0.9, roughness: 0.2, transparent: true, opacity: 0.65 })
  );
  cap.position.y = -0.41;
  root.add(cap);
  glowSprite = new Sprite(new SpriteMaterial({
    map: createGlowTexture('#00f0ff'), transparent: true, opacity: 0.28, blending: AdditiveBlending
  }));
  glowSprite.scale.set(0.65, 0.65, 1.0);
  glowSprite.position.y = 0.14;
  root.add(glowSprite);
  filament = new Mesh(
    new CylinderGeometry(0.008, 0.008, 0.12, 16),
    new MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.65 })
  );
  filament.position.y = 0.14;
  root.add(filament);

  /* 取景：装配包围盒（点云 ∪ 玻璃罩 ±0.5）高 ×1.7 = 可视高，与 CSS --b3d 的 1.7 一致，
     灯泡恒占画布 1/1.7 → 屏幕身量精确等于原 PNG 的 min(43vh,58vw) */
  const top = Math.max(maxY, 0.5), bottom = Math.min(minY, -0.5);
  root.position.y = -(top + bottom) / 2;
  camera.fov = 2 * Math.atan((top - bottom) * 1.7 / 2 / 4.6) * 180 / Math.PI;
  camera.updateProjectionMatrix();

  composer = new EffectComposer(renderer);
  composer.addPass(new RenderPass(scene, camera));
  composer.addPass(new UnrealBloomPass(new Vector2(canvas3d.clientWidth, canvas3d.clientHeight), 0.6, 0.45, 0.85));
  composer.addPass(new OutputPass());
  composer.addPass(new ShaderPass(AlphaFromLumaShader));

  onResize3D();
  addEventListener('resize', onResize3D);
  document.body.classList.add('webgl3d');
  window.__bulb3dActive = true;

  if (PRM) renderTick(FROZEN_T, 0.016);              /* 静态一帧，不进循环 */
  else requestAnimationFrame(loop3d);
}

function onResize3D() {
  const s = canvas3d.clientWidth;                    /* 方形：clientWidth == clientHeight */
  renderer.setSize(s, s, false);
  composer.setSize(s, s);
  if (PRM) renderTick(FROZEN_T, 0.016);
}

let last3d = 0;
function loop3d(nowMs) {
  requestAnimationFrame(loop3d);
  const t = nowMs / 1000;
  renderTick(t, Math.min(Math.max(t - last3d, 0.001), 0.05));
  last3d = t;
}

function renderTick(t, dt) {
  const u = particleMaterial.uniforms;
  u.uTime.value = t;
  if (!PRM) {                                        /* 排斥场：yun animate() 原逻辑 */
    if (pointerInCanvas) cursorRayDir.set(mouseNDC.x, mouseNDC.y, 0.5).unproject(camera).sub(camera.position).normalize();
    u.uMouseOrigin.value.copy(camera.position);
    u.uMouseDir.value.lerp(cursorRayDir, 1 - Math.exp(-9 * dt)).normalize();
    const target = pointerInCanvas ? 1 : 0;
    const ease = target > u.uRepelStrength.value ? 5 : 2.2;
    u.uRepelStrength.value += (target - u.uRepelStrength.value) * (1 - Math.exp(-ease * dt));
  }
  root.rotation.y = t * 0.02;                        /* 缓慢自旋 */
  const k = (t - surgeT0) / 0.7;                     /* 核心喷发：0.7s 正弦包络（yun surge 的无 GSAP 版） */
  const env = (k >= 0 && k <= 1) ? Math.sin(Math.PI * k) : 0;
  filament.scale.setScalar(1 + 0.8 * env);
  glowSprite.material.opacity = 0.28 + 0.27 * env;
  pointLight.intensity = 1.5 + 1.3 * env;
  composer.render();
}

try {
  initBulb3D();
} catch (err) {
  console.warn('bulb3d 回退为 PNG 灯泡：', err && err.message);
  /* 不加 body.webgl3d → 原 #halo + #bulb 照常呈现，两段式交互不受影响 */
}
```

- [ ] **Step 4: 验证——三张探针截图（无头 Chrome 一次性进程）**

```bash
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
SNAP=/private/tmp/claude-501/-Users-cc-newapi628/c510bd99-9f56-46e0-a6a7-73a693f4abfe/scratchpad
"$CHROME" --headless=new --hide-scrollbars --window-size=1920,990 --virtual-time-budget=5000 \
  --screenshot="$SNAP/t2-pre.png" "file:///Users/cc/newapi628/bulb-orbit/index.html"
"$CHROME" --headless=new --hide-scrollbars --window-size=1920,990 --virtual-time-budget=7000 \
  --screenshot="$SNAP/t2-final.png" "file:///Users/cc/newapi628/bulb-orbit/index.html#final"
"$CHROME" --headless=new --hide-scrollbars --window-size=1920,990 --virtual-time-budget=5000 \
  --screenshot="$SNAP/t2-nowebgl.png" "file:///Users/cc/newapi628/bulb-orbit/index.html?nowebgl=1"
```

（注意**不加** `--disable-gpu`，无头 WebGL 需 SwiftShader/GPU。）用 Read 逐张查看：

- `t2-pre.png`：黑场中央是**点阵粒子灯泡**（可见离散光点组成的灯泡+发光底座，非平滑 PNG 玻璃泡），下方提示文字仍在；
- `t2-final.png`：全景——粒子灯泡居中于原位，2D 轨道穿越、24 芯片环绕、顶部文字，均与旧版 `阶段2-点击点亮-全景与文字.png` 布局一致；灯泡身量与旧 PNG 相当（对读两图）；
- `t2-nowebgl.png`：回退生效——显示的是**旧版平滑 PNG 灯泡 + CSS 光晕**。

- [ ] **Step 5: 验证——运行时探针与控制台零报错（playwright-cli，用完即关）**

playwright-cli 打开 `file:///Users/cc/newapi628/bulb-orbit/index.html`：evaluate `window.__bulb3dActive === true`、`document.body.classList.contains('webgl3d') === true`，并读取控制台确认无 error（`bulb3d 回退` warn 也不应出现）。关闭后：

```bash
ps aux | grep -iE 'headless|playwright' | grep -v grep   # 期望无输出
```

若灯泡身量/垂直位置与旧 PNG 有可感偏差：微调 `root.position.y`（±0.05 步进）或把 1.7 系数同步改两处（CSS `--b3d` 与 `camera.fov` 行），重截 `t2-final.png` 对读，直至肉眼对齐。

- [ ] **Step 6: 提交**

```bash
cd /Users/cc/newapi628
git add bulb-orbit/index.html
git commit -m "feat(bulb-orbit): 3D 粒子灯泡装配替换 PNG（yun 移植：点云+玻璃罩+灯丝+辉光+Bloom，透明画布亮度转alpha，含 no-webgl 回退）"
```

---

### Task 3: 交互整合（鼠标排斥 + 点亮/重放核心喷发）

**Files:**
- Modify: `bulb-orbit/index.html`（仅 BULB3D-CODE 手写区末尾追加；不动 Task 2 的渲染代码，不动主脚本）

**Interfaces:**
- Consumes：Task 2 的 `canvas3d`、`mouseNDC`、`pointerInCanvas`、`surgeT0`、`PRM`、`window.__bulb3dActive`。
- Produces：`window.__bulb3d = { surge, get surgeT0, get repel }`（调试/验证句柄）。
- 与主脚本的耦合协议：**零改动**——主脚本的 `ignite()` 挂在 `pointerdown` 冒泡段；本区用**捕获段**监听同一事件，先于 `ignite()` 读到 `body.pre` 状态判定「点亮瞬间」。

- [ ] **Step 1: 在 `try { initBulb3D(); } catch ...` 之前追加交互代码**

```js
/* ---------- 交互：排斥场坐标 + 核心喷发触发（捕获段先于主脚本 ignite 读取 pre 状态） ---------- */
addEventListener('mousemove', e => {
  const r = canvas3d.getBoundingClientRect();
  mouseNDC.x = ((e.clientX - r.left) / r.width) * 2 - 1;
  mouseNDC.y = -((e.clientY - r.top) / r.height) * 2 + 1;
  /* 稍溢出画布仍算在场内，排斥的高斯衰减自然收尾；远离后 renderTick 缓释回流 */
  pointerInCanvas = Math.abs(mouseNDC.x) < 1.15 && Math.abs(mouseNDC.y) < 1.15;
});
document.addEventListener('mouseleave', () => { pointerInCanvas = false; });

function surge() {
  if (!PRM && window.__bulb3dActive) surgeT0 = performance.now() / 1000;
}
addEventListener('pointerdown', e => {
  if (!window.__bulb3dActive) return;
  if (document.body.classList.contains('pre')) { surge(); return; }   /* 点亮瞬间：3D 版迸发 */
  const r = canvas3d.getBoundingClientRect();
  const nx = ((e.clientX - r.left) / r.width) * 2 - 1;
  const ny = -((e.clientY - r.top) / r.height) * 2 + 1;
  if (nx * nx + ny * ny < 0.55 * 0.55) surge();                        /* lit：点灯泡区域重放 */
}, true);

window.__bulb3d = {
  surge,
  get surgeT0() { return surgeT0; },
  get repel() { return particleMaterial ? particleMaterial.uniforms.uRepelStrength.value : -1; }
};
```

- [ ] **Step 2: 验证——排斥与喷发的确定性探针（playwright-cli）**

playwright-cli 打开 `file:///Users/cc/newapi628/bulb-orbit/index.html`（1920×990），依次：

1. 把鼠标移到视口中央灯泡处（约 960, 564）等 0.8s → evaluate `window.__bulb3d.repel` 期望 `> 0.5`（排斥场已激活）；截图 `t3-repel.png`，Read 对比 `t2-pre.png`：光标处粒子被推开成环；
2. 把鼠标移到 (100, 100)（画布外）等 1.5s → evaluate `window.__bulb3d.repel` 期望 `< 0.4` 且在下降（回流）；
3. evaluate `window.__bulb3d.surgeT0` 期望 `-1e9`（尚未触发）→ 在 (960, 564) 点击 → evaluate `document.body.classList.contains('lit')===true` 且 `window.__bulb3d.surgeT0 > 0`（点亮 + 迸发同时发生）；等 2.5s 截图 `t3-lit.png`：轨道/芯片/文字全景就位；
4. 记 `s1 = surgeT0` → 点击 (100, 900)（远离灯泡）→ `surgeT0 === s1`（未误触发）→ 点击 (960, 564) → `surgeT0 > s1`（灯泡区域重放成功）；
5. 控制台无 error。

关闭 playwright 并 `ps aux | grep -iE 'headless|playwright' | grep -v grep` 复核零残留。

- [ ] **Step 3: 验证——`#final` 直达不自动迸发**

playwright-cli 打开 `file:///Users/cc/newapi628/bulb-orbit/index.html#final`，evaluate `window.__bulb3d.surgeT0` 期望 `-1e9`、`document.body.className === 'lit webgl3d'`（顺序不限，两类都在）。关闭并复核进程。

- [ ] **Step 4: 提交**

```bash
cd /Users/cc/newapi628
git add bulb-orbit/index.html
git commit -m "feat(bulb-orbit): 灯泡交互——鼠标排斥流体接入两段式，点亮/点灯泡触发 3D 核心喷发"
```

---

### Task 4: 验收、交付截图与文档更新

**Files:**
- Modify: `bulb-orbit/阶段1-初始-只亮灯泡.png`、`bulb-orbit/阶段2-点击点亮-全景与文字.png`（重截覆盖）
- Modify: `bulb-orbit/交互修改文档.md`（追加粒子灯泡章节 + 修订涉灯泡的两处表述）
- Modify: `RETRO.md`（仅当实施中踩坑时记录）

**Interfaces:**
- Consumes：Task 2/3 完成后的 `index.html` 全部行为。

- [ ] **Step 1: 重截两张交付图（覆盖旧图）**

```bash
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
cd /Users/cc/newapi628/bulb-orbit
"$CHROME" --headless=new --hide-scrollbars --window-size=1920,990 --virtual-time-budget=5000 \
  --screenshot="阶段1-初始-只亮灯泡.png" "file:///Users/cc/newapi628/bulb-orbit/index.html"
"$CHROME" --headless=new --hide-scrollbars --window-size=1920,990 --virtual-time-budget=7000 \
  --screenshot="阶段2-点击点亮-全景与文字.png" "file:///Users/cc/newapi628/bulb-orbit/index.html#final"
```

- [ ] **Step 2: 对照验收（Read 三方对读）**

- Read `阶段1-初始-只亮灯泡.png` + `阶段2-点击点亮-全景与文字.png`；
- 对照 scratchpad `yun/hero-logo-satellite-final.png`：粒子灯泡形象（点阵质感、青蓝配色、发光底座、泛光柔度）还原到位；
- 对照 git 里旧版阶段 2 图（`git show HEAD~3:bulb-orbit/... > /tmp` 不可用——旧图未入库，用 Task 2 留存的对读结论即可）：布局/文字/芯片无回归。
- 核对设计 §9 验收表逐条通过（断网双击项：成品无任何网络引用，`grep -c "https://" bulb-orbit/index.html` 期望 0——SVG 命名空间 `http://www.w3.org/2000/svg` 不算，检查仅 `https://`）。

- [ ] **Step 3: 更新 `bulb-orbit/交互修改文档.md`**

(a) §三表格中「灯泡 `#bulb` + 光晕 `#halo`」一行的两格改为：「✅ 3D 粒子灯泡（40000 点云）可见，呼吸+闪烁+自旋+鼠标排斥」/「✅ 点击瞬间 3D 核心喷发（灯丝脉冲+辉光增亮+点光闪烁，0.7s）后回常驻」；
(b) §四时间轴中 `#halo` 迸发一行改为「`t=0` 3D 核心喷发：灯丝 scale 1→1.8→1、辉光 0.28→0.55→0.28、点光 1.5→2.8→1.5，0.7s 正弦包络」；
(c) 文末新增章节：

```markdown
---

## 十、2026-07-07 粒子灯泡替换（yun 移植）

灯泡形象由 `bulb.png` 换为 WebGL 粒子点云（源自 GitHub wei500L/yun，three@0.160.0 全内嵌单文件）：
40000 粒子 + 玻璃罩/灯座/灯丝/辉光 + UnrealBloom；新增动效——鼠标排斥流体（光标推开粒子成环、
离开回流）、点亮/点击灯泡触发核心喷发；`?nowebgl` 或 WebGL 不可用自动回退 PNG 灯泡。
设计与实施：`docs/superpowers/specs/2026-07-07-bulb-orbit-particle-bulb-design.md`、
`docs/superpowers/plans/2026-07-07-bulb-orbit-particle-bulb.md`。
重截图命令不变（见 §七）；重生 vendor/数据区块：`python3 tools/build-single.py`。
```

- [ ] **Step 4: W6 收尾复核**

```bash
ps aux | grep -iE 'headless|playwright|http.server' | grep -v grep   # 期望无输出
```

- [ ] **Step 5: 踩坑登记（条件步骤）**

若 Task 1-4 过程中出现返工级问题（如 vendor 拼接正则误伤、无头 WebGL 截图全黑需换旗标、Bloom alpha 方案调整），按 `RETRO.md` 维护规则登记现象/根因/解决方案；无则跳过。

- [ ] **Step 6: 最终提交**

```bash
cd /Users/cc/newapi628
git add bulb-orbit/交互修改文档.md
# 若 Step 5 改了 RETRO.md：git add RETRO.md
git commit -m "docs(bulb-orbit): 粒子灯泡替换交付——交互文档更新，两张阶段截图重截（截图不入库）"
```

---

## Self-Review 记录

- **Spec 覆盖**：§4 画布架构/亮度转alpha → Task 2；§5 装配清单 → Task 2 Step 3（参数逐字）；§6 五行交互表 → pre 排斥(T3 mousemove 全程有效) / 点亮喷发(T3 捕获段 pre 分支) / lit 重放(T3 命中圆) / #final 不自动迸发(T3 Step 3 验证) / reduced-motion(T2 `PRM` 冻结 + surge 短路)；§7 文件结构 → Task 1；§8 降级链 → T2 try/catch + `?nowebgl` + DPR 上限 + 后台 rAF 自动暂停（浏览器原生行为）；§9 验收 → Task 4；备用 .bin → Task 1 Step 1。
- **占位符扫描**：无 TBD/TODO；所有代码步骤给出完整代码；条件步骤（RETRO）有明确触发条件。
- **类型/命名一致性**：`canvas3d/mouseNDC/pointerInCanvas/surgeT0/PRM/particleMaterial/glowSprite/filament/pointLight/root/renderTick/loop3d/onResize3D/initBulb3D/surge/__bulb3dActive/__bulb3dVendorOK/__bulbDataB64Len/DENGPAO_B64_CHUNKS` 在 T1/T2/T3 的 Interfaces 与代码中逐一核对一致；`window.__bulb3d` 的 getter 名（`surgeT0`/`repel`）与 T3 验证步骤一致。
