// @ts-nocheck
/* WeDream 落地页样式 —— 移植自 bulb-orbit/index.html 的 <style>（8–641），仅顶部 reset 收敛到 .wd-landing-root 作用域。
   以「挂载时注入 <style>、卸载即移除」的方式使用（见 index.tsx），确保只在 /landing-react 路由存在，不永久污染全站。 */
export const LANDING_CSS = `
  .wd-landing-root, .wd-landing-root * { margin:0; padding:0; box-sizing:border-box; }
  .wd-landing-root { background:#020610; overflow-x:hidden; position:relative; min-height:100vh;
    font-family:"PingFang SC","HarmonyOS Sans SC","MiSans","Microsoft YaHei",system-ui,sans-serif; }
  .wd-landing-root img, .wd-landing-root svg { display:block; }

  /* ---------- background: center glow + faint grid + vignette ---------- */
  #bg {
    position:fixed; inset:0; z-index:0; pointer-events:none;
    background:
      radial-gradient(ellipse 40% 44% at 62% 46%, rgba(48,132,238,.18), transparent 66%),
      radial-gradient(ellipse 52% 46% at 83% 18%, rgba(126,92,232,.13), transparent 70%),
      radial-gradient(ellipse 48% 52% at 24% 84%, rgba(38,120,214,.10), transparent 72%),
      radial-gradient(circle at 60% 46%, #0a1730 0%, #081226 44%, #040a1a 76%, #020610 100%);
  }
  #aurora {
    position:fixed; inset:-25%; z-index:0; pointer-events:none; opacity:.7;
    background:
      radial-gradient(ellipse 38% 40% at 34% 34%, rgba(96,124,255,.11), transparent 60%),
      radial-gradient(ellipse 42% 38% at 72% 66%, rgba(158,96,244,.09), transparent 62%),
      radial-gradient(ellipse 34% 34% at 56% 42%, rgba(40,200,220,.07), transparent 58%);
    filter:blur(40px);
    animation:auroraDrift 34s ease-in-out infinite alternate;
  }
  @keyframes auroraDrift {
    0%   { transform:translate(0,0) scale(1); }
    100% { transform:translate(3.5%,-2.5%) scale(1.1); }
  }
  @media (prefers-reduced-motion: reduce) { #aurora { animation:none; } }
  #grid {
    position:fixed; inset:0; z-index:0; pointer-events:none;
    background:
      linear-gradient(rgba(120,180,255,.028) 1px, transparent 1px),
      linear-gradient(90deg, rgba(120,180,255,.028) 1px, transparent 1px);
    background-size:56px 56px; background-position:center;
    -webkit-mask-image:radial-gradient(circle at 50% 57%, #000 0%, transparent 78%);
    mask-image:radial-gradient(circle at 50% 57%, #000 0%, transparent 78%);
  }
  /* 原版的 .ring 雷达环已按用户要求移除（见 orbit.ts 同名注释） */
  #vignette {
    position:fixed; inset:0; z-index:12; pointer-events:none;
    background:radial-gradient(ellipse 75% 70% at 50% 57%, transparent 55%, rgba(2,5,12,.55) 100%);
  }

  #stardust, #stardust2 { position:fixed; inset:0; z-index:0; pointer-events:none; }
  #stardust {
    opacity:.95;
    background-image:
      radial-gradient(2.3px 2.3px at 40px 60px,   rgba(216,241,255,1),   transparent 64%),
      radial-gradient(2px 2px at 180px 150px,     rgba(168,210,255,.95), transparent 64%),
      radial-gradient(2.5px 2.5px at 90px 240px,  rgba(222,244,255,1),   transparent 64%),
      radial-gradient(2.1px 2.1px at 260px 95px,  rgba(178,214,255,.92), transparent 64%),
      radial-gradient(2.3px 2.3px at 325px 300px, rgba(208,236,255,.9),  transparent 64%),
      radial-gradient(1.9px 1.9px at 150px 330px, rgba(192,226,255,.88), transparent 64%);
    background-size:300px 300px;
    animation:stardustA 150s linear infinite;
  }
  #stardust2 {
    opacity:.7;
    background-image:
      radial-gradient(1.8px 1.8px at 120px 80px,  rgba(152,207,255,.9),  transparent 64%),
      radial-gradient(2px 2px at 300px 210px,     rgba(132,216,255,.85), transparent 64%),
      radial-gradient(1.7px 1.7px at 210px 350px, rgba(198,230,255,.8),  transparent 64%),
      radial-gradient(1.6px 1.6px at 60px 430px,  rgba(162,210,255,.75), transparent 64%),
      radial-gradient(1.7px 1.7px at 470px 120px, rgba(142,214,255,.75), transparent 64%);
    background-size:460px 460px;
    animation:stardustB 240s linear infinite, stardustTwinkle 7s ease-in-out infinite;
  }
  @keyframes stardustA { from{background-position:0 0;} to{background-position:-20px -360px;} }
  @keyframes stardustB { from{background-position:0 0;} to{background-position:30px -540px;} }
  @keyframes stardustTwinkle { 0%,100%{opacity:.5;} 50%{opacity:.3;} }
  @media (prefers-reduced-motion: reduce){ #stardust, #stardust2 { animation:none; } }

  .p {
    position:absolute; z-index:1; pointer-events:none; border-radius:50%;
    will-change:transform,opacity; opacity:var(--o1);
    animation:
      drift var(--dur) ease-in-out var(--delay) infinite alternate,
      twinkle var(--tdur) ease-in-out var(--delay) infinite alternate;
  }
  @keyframes drift   { from { transform:translate3d(0,0,0); } to { transform:translate3d(var(--dx),var(--dy),0); } }
  @keyframes twinkle { from { opacity:var(--o1); } to { opacity:var(--o2); } }

  #orbits { position:fixed; inset:0; z-index:1; pointer-events:none; overflow:visible; }
  #orbits line { fill:none; }

  .fp {
    position:absolute; left:var(--cx); top:var(--cy); z-index:1; pointer-events:none;
    width:3.5px; height:3.5px; margin:-1.75px 0 0 -1.75px; border-radius:50%;
    background:#dff1ff; will-change:transform,opacity;
    box-shadow:0 0 6px 2px rgba(140,210,255,.85), 0 0 14px 4px rgba(80,160,255,.35);
  }

  .ray {
    position:absolute; left:var(--cx); top:var(--cy); z-index:1; pointer-events:none;
    width:10px; margin-left:-5px; transform-origin:50% 100%;
    background:radial-gradient(ellipse 58% 104% at 50% 100%,
      rgba(130,195,255,.32) 0%, rgba(130,195,255,.10) 46%, transparent 74%);
    opacity:var(--o1);
    animation:rayPulse var(--pdur) ease-in-out var(--pdelay) infinite alternate;
  }
  @keyframes rayPulse { from { opacity:var(--o1); } to { opacity:var(--o2); } }

  #halo {
    position:absolute; left:var(--cx); top:var(--cy); z-index:5; pointer-events:none;
    will-change:transform,opacity;
    width:54vmin; height:54vmin; margin:-27vmin 0 0 -27vmin; border-radius:50%;
    background:radial-gradient(circle,
      rgba(110,205,255,.38) 0%, rgba(60,150,255,.20) 34%,
      rgba(30,95,230,.09) 56%, rgba(20,80,220,.03) 70%, transparent 80%);
    animation:breathe 4s ease-in-out infinite;
  }
  @keyframes breathe { 0%,100% { opacity:.7; } 50% { opacity:1; } }
  #bulb {
    position:absolute; left:var(--cx); top:var(--cy); z-index:6; pointer-events:none;
    height:min(43vh, 58vw); width:auto;
    transform:translate(-50%,-50%);
    user-select:none; -webkit-user-drag:none;
    filter:drop-shadow(0 0 24px rgba(70,170,255,.35));
  }
  #scene3d {
    position:absolute; inset:0; width:100%; height:100%; z-index:6; pointer-events:none;
    display:none;
  }
  body.webgl3d #scene3d { display:block; }
  body.webgl3d #halo, body.webgl3d #bulb { display:none; }

  .wd-landing-root { --chip:52px; --cy:42%; --cx:50%; --b3d:400px; }
  .chip {
    position:absolute; left:var(--cx); top:var(--cy);
    width:var(--chip); height:var(--chip);
    margin:calc(var(--chip) / -2) 0 0 calc(var(--chip) / -2);
    will-change:transform,opacity;
  }
  .chip.main {
    width:calc(var(--chip) * 1.12); height:calc(var(--chip) * 1.12);
    margin:calc(var(--chip) * 1.12 / -2) 0 0 calc(var(--chip) * 1.12 / -2);
  }
  .chip.alt {
    width:calc(var(--chip) * 0.92); height:calc(var(--chip) * 0.92);
    margin:calc(var(--chip) * 0.92 / -2) 0 0 calc(var(--chip) * 0.92 / -2);
  }
  .shell { position:absolute; inset:0; border-radius:50%; transition:transform .28s ease; }
  .shell::before {
    content:''; position:absolute; inset:0; border-radius:50%;
    background:radial-gradient(circle at 50% 44%,
      rgba(20,30,55,.90) 0%, rgba(14,22,44,.94) 55%, rgba(6,11,24,.97) 100%);
    border:1.5px solid rgba(120,180,255,.6);
    box-shadow:
      0 0 12px rgba(80,160,255,.42), 0 0 30px rgba(50,120,255,.18),
      inset 0 0 14px rgba(90,170,255,.16), inset 0 -5px 12px rgba(0,0,0,.5);
    transition:box-shadow .28s ease, border-color .28s ease;
  }
  .shell::after {
    content:''; position:absolute; left:10%; right:10%; top:4%; height:48%;
    border-radius:50%; pointer-events:none;
    border:1.4px solid transparent; border-top-color:rgba(225,242,255,.5);
    background:linear-gradient(to bottom, rgba(215,238,255,.24), transparent 70%);
    transform:rotate(-8deg);
  }
  .disc {
    position:absolute; inset:0; display:grid; place-items:center;
    animation:spin var(--spin) linear infinite;
    animation-direction:var(--dir);
  }
  @keyframes spin { to { transform:rotate(1turn); } }
  .disc svg { width:54%; height:54%; }
  .disc img { width:62%; height:62%; object-fit:contain; }
  .chip:hover .shell { transform:scale(1.1); }
  .chip:hover .shell::before {
    border-color:rgba(170,215,255,.9);
    box-shadow:
      0 0 20px rgba(110,185,255,.7), 0 0 48px rgba(70,145,255,.4),
      inset 0 0 16px rgba(110,185,255,.28), inset 0 -5px 12px rgba(0,0,0,.5);
  }

  #hero {
    display:flex; flex-direction:column; align-items:flex-start; gap:14px;
    text-align:left; pointer-events:none;
  }
  #badge {
    display:inline-flex; align-items:center; gap:7px;
    padding:7px 16px; border-radius:999px;
    border:1px solid rgba(90,190,255,.4);
    background:rgba(10,26,54,.55);
    box-shadow:0 0 14px rgba(40,130,255,.18), inset 0 0 10px rgba(80,170,255,.08);
    color:#9fd8ff; font-size:12.5px; letter-spacing:.18em;
  }
  #badge svg { width:12px; height:12px; }
  #hero h1 { display:flex; flex-direction:column; gap:8px; margin:4px 0 0; font-weight:normal; }
  /* 标题两行 = 渐变文字;选中模型时渐变色平滑过渡到该模型色系。
     @property 让「渐变里的颜色」可 transition(普通 CSS 变量做不到平滑渐变过渡);
     不支持 @property 的老浏览器降级为即时切换,渐变本身仍在。 */
  @property --wd-g1 { syntax: '<color>'; inherits: true; initial-value: #3ecfff; }
  @property --wd-g2 { syntax: '<color>'; inherits: true; initial-value: #8f7bff; }
  /* 品牌名「WeDream AI」= 固定渐变,不随选中变色(用户要求所有 WeDream AI 名称不变色) */
  .hl1 {
    font-size:clamp(28px, 4.4vw, 52px); font-weight:700;
    letter-spacing:.1em; padding-left:.1em;
    background:linear-gradient(96deg, #3ecfff, #8f7bff);
    -webkit-background-clip:text; background-clip:text;
    color:transparent; -webkit-text-fill-color:transparent;
    filter:drop-shadow(0 0 24px rgba(90,170,255,.3));
  }
  .hl2 {
    font-size:clamp(38px, 6vw, 72px); font-weight:800;
    letter-spacing:.05em; padding-left:.05em; line-height:1.15;
    background:linear-gradient(96deg, var(--wd-g1), var(--wd-g2));
    -webkit-background-clip:text; background-clip:text;
    color:transparent; -webkit-text-fill-color:transparent;
    filter:drop-shadow(0 0 22px rgba(60,150,255,.35));
    transition:--wd-g1 .55s ease, --wd-g2 .55s ease;
  }
  #sub {
    margin-top:2px; color:rgba(160,184,216,.88);
    font-size:clamp(13px, 1.5vw, 16px);
    letter-spacing:.18em;
  }
  .rise { opacity:0; transform:translateY(16px); }
  body.lit .rise { animation:rise .9s cubic-bezier(.22,.7,.25,1) var(--rd,0s) forwards; }
  @keyframes rise { to { opacity:1; transform:translateY(0); } }

  body.lit #halo {
    animation:igniteBurst 1.15s cubic-bezier(.3,.7,.3,1) both, breathe 4s ease-in-out 1.15s infinite;
  }
  @keyframes igniteBurst {
    0%   { opacity:.7;  transform:scale(.96); }
    28%  { opacity:1;   transform:scale(1.16); }
    100% { opacity:.85; transform:scale(1); }
  }

  @media (prefers-reduced-motion: reduce) {
    body.lit .rise { animation:none; opacity:1; transform:none; }
    .p, .ray, #halo, .disc, body.lit #halo { animation:none; }
    #halo { opacity:.85; }
    .reveal { opacity:1; transform:none; }
  }

  #hud {
    position:absolute; right:clamp(12px,3%,48px); top:50%; z-index:16;
    width:min(84%,320px); pointer-events:none;
    padding:18px 20px 16px; border-radius:16px;
    background:rgba(9,17,36,.74); backdrop-filter:blur(16px); -webkit-backdrop-filter:blur(16px);
    border:1px solid rgba(120,180,255,.22); border-left:3px solid #00f0ff;
    box-shadow:0 10px 44px rgba(0,8,32,.55), inset 0 0 20px rgba(80,170,255,.05);
    opacity:0; visibility:hidden; transform:translate(20px,-50%);
    transition:opacity .35s ease, transform .45s cubic-bezier(.22,.7,.25,1), visibility 0s linear .45s;
  }
  #hud.show { opacity:1; visibility:visible; transform:translate(0,-50%);
    transition:opacity .35s ease, transform .45s cubic-bezier(.22,.7,.25,1); }
  #hud .hud-prov { display:flex; align-items:center; gap:8px; color:#9fd8ff; font-size:12.5px; letter-spacing:.12em; }
  #hud .hud-dot { width:8px; height:8px; border-radius:50%; background:#00f0ff; box-shadow:0 0 8px currentColor; flex:none; }
  #hud .hud-name { margin-top:8px; color:#f2f7ff; font-size:19px; font-weight:700; }
  #hud .hud-desc { margin-top:10px; color:rgba(198,214,238,.9); font-size:13px; line-height:1.6; }
  #hud .hud-scene { margin-top:8px; color:rgba(150,175,210,.8); font-size:12px; line-height:1.5; }
  #hud .hud-tele { margin-top:12px; padding-top:10px; border-top:1px solid rgba(120,180,255,.14);
    color:#8fd0ff; font-size:11.5px; letter-spacing:.06em; }

  .landing-scene {
    position:relative;
    background:
      radial-gradient(ellipse 72% 620px at 78% calc(100vh + 360px),rgba(62,207,255,.065),transparent 72%),
      radial-gradient(ellipse 68% 720px at 14% calc(100vh + 720px),rgba(143,123,255,.05),transparent 74%),
      linear-gradient(180deg,
        transparent 0,
        rgba(2,6,16,.08) calc(100vh - 220px),
        rgba(4,11,26,.18) calc(100vh + 300px),
        rgba(3,9,22,.14) 72%,
        rgba(2,6,16,.06) 100%);
  }
  .hero-stage { position:relative; width:100%; height:100vh; overflow:hidden; display:flex; align-items:stretch; }
  .hero-left { flex:0 0 40%; min-width:0; position:relative; z-index:15;
    display:flex; flex-direction:column; justify-content:center; gap:20px;
    padding:0 clamp(24px,4vw,80px); pointer-events:none; }
  .hero-right { flex:1 1 0; min-width:0; position:relative; }
  .hero-right #orbits { position:absolute; inset:0; }
  .hero-visual { position:absolute; inset:0; transform-origin:52% 50%;
    transition:transform .55s cubic-bezier(.22,.7,.25,1); }
  .hero-right.card-open .hero-visual { transform:translateX(-15%) scale(.88); }

  .features { display:flex; flex-wrap:wrap; gap:20px 28px; margin-top:4px; max-width:520px; }
  .feature { display:flex; align-items:flex-start; gap:11px; width:calc(50% - 14px); min-width:150px; }
  .feature-ico { flex:none; width:34px; height:34px; display:grid; place-items:center; border-radius:9px;
    background:rgba(60,150,255,.14); border:1px solid rgba(120,180,255,.22); }
  .feature-ico svg { width:19px; height:19px; }
  .feature-name { color:#eaf2ff; font-size:14.5px; font-weight:700; }
  .feature-desc { margin-top:3px; color:rgba(160,184,216,.78); font-size:12px; line-height:1.5; }
  .cta { display:flex; flex-wrap:wrap; gap:14px; margin-top:8px; pointer-events:auto; }
  .btn { display:inline-flex; align-items:center; justify-content:center; padding:12px 28px; border-radius:10px;
    font-size:14.5px; font-weight:600; cursor:pointer; text-decoration:none; white-space:nowrap;
    transition:transform .2s ease, box-shadow .2s ease, background .2s ease, border-color .2s ease; }
  .btn-primary { background:linear-gradient(94deg,#3b82f6,#4d9bff); color:#fff; box-shadow:0 6px 20px rgba(50,120,255,.42); }
  .btn-primary:hover { transform:translateY(-2px); box-shadow:0 10px 30px rgba(50,120,255,.58); }
  .btn-ghost { background:rgba(255,255,255,.04); color:#cfe0f5; border:1px solid rgba(140,180,240,.36); }
  .btn-ghost:hover { background:rgba(255,255,255,.09); border-color:rgba(140,180,240,.62); }

  /* ---------- 落地页顶栏 + 悬浮客服球 ---------- */
  .lp-topbar { position:fixed; top:0; left:0; right:0; z-index:60; display:flex; align-items:center; justify-content:space-between; padding:16px clamp(20px,4vw,52px); pointer-events:none; }
  .lp-topbar > * { pointer-events:auto; }
  .lp-logo { display:flex; align-items:center; gap:10px; font-size:19px; font-weight:800; letter-spacing:.02em; color:#eaf3ff; }
  .lp-logo span { color:#eaf3ff; } /* 页眉品牌名固定色,不随选中变色 */
  .lp-logo .lp-mark-img { flex:none; width:40px; height:40px; border-radius:10px; object-fit:cover; box-shadow:0 0 16px -3px rgba(62,207,255,.4); }
  .lp-actions { display:flex; align-items:center; gap:12px; }
  .lp-kefu-btn, .lp-lang { display:inline-flex; align-items:center; gap:7px; height:40px; padding:0 15px; border-radius:10px; background:rgba(255,255,255,.06); border:1px solid rgba(140,180,240,.22); color:#cfe0f5; font-size:14px; font-family:inherit; cursor:pointer; transition:background .2s,border-color .2s; }
  .lp-kefu-btn:hover, .lp-lang:hover { background:rgba(255,255,255,.12); border-color:rgba(140,180,240,.5); }
  .lp-lang { padding:0 13px; gap:6px; }
  .lp-lang-txt { font-size:13px; font-weight:600; letter-spacing:.02em; }
  .lp-kefu-btn svg, .lp-lang svg { width:17px; height:17px; fill:none; stroke:currentColor; stroke-width:1.7; stroke-linecap:round; stroke-linejoin:round; }
  .lp-start { display:inline-flex; align-items:center; gap:7px; height:40px; padding:0 20px; border-radius:10px; font-size:14.5px; font-weight:650; color:#04121a; background:linear-gradient(94deg,#3ecfff,#4d9bff 60%,#8f7bff); text-decoration:none; box-shadow:0 8px 24px -8px rgba(62,207,255,.7); transition:transform .2s,box-shadow .2s; }
  .lp-start:hover { transform:translateY(-2px); box-shadow:0 12px 32px -8px rgba(62,207,255,.9); }
  .lp-start svg { width:16px; height:16px; fill:none; stroke:currentColor; stroke-width:2; stroke-linecap:round; stroke-linejoin:round; transition:transform .2s; }
  .lp-start:hover svg { transform:translateX(3px); }
  @media (max-width:640px){ .lp-logo span:last-child { display:none; } .lp-kefu-btn span { display:none; } .lp-kefu-btn { padding:0; width:40px; justify-content:center; } }

  .lp-fab { position:fixed; bottom:26px; right:26px; z-index:60; width:56px; height:56px; border-radius:50%; display:grid; place-items:center; cursor:pointer; border:none; background:linear-gradient(135deg,#3ecfff,#4d9bff); box-shadow:0 10px 30px -6px rgba(62,207,255,.7); transition:transform .2s,box-shadow .2s; }
  .lp-fab:hover { transform:translateY(-3px) scale(1.05); box-shadow:0 16px 40px -6px rgba(62,207,255,.9); }
  .lp-fab svg { width:26px; height:26px; fill:none; stroke:#04121a; stroke-width:1.8; stroke-linecap:round; stroke-linejoin:round; }

  /* ---------- 在线客服弹窗（移植自原版 615–640）---------- */
  .lp-kefu-modal { position:fixed; inset:0; z-index:100; display:flex; align-items:center; justify-content:center; padding:20px; background:rgba(2,5,12,.72); backdrop-filter:blur(6px); -webkit-backdrop-filter:blur(6px); opacity:0; visibility:hidden; transition:opacity .25s,visibility .25s; }
  .lp-kefu-modal.show { opacity:1; visibility:visible; }
  .lp-kefu-card { width:min(92vw,420px); border-radius:20px; overflow:hidden; background:rgba(10,18,36,.97); border:1px solid rgba(120,180,255,.2); box-shadow:0 30px 80px -20px rgba(0,0,0,.7); transform:translateY(14px) scale(.98); transition:transform .25s; }
  .lp-kefu-modal.show .lp-kefu-card { transform:none; }
  .lp-kefu-head { display:flex; align-items:center; gap:12px; padding:20px 22px; border-bottom:1px solid rgba(120,180,255,.12); }
  .lp-kh-ic { flex:none; width:40px; height:40px; border-radius:11px; display:grid; place-items:center; background:rgba(62,207,255,.12); color:#3ecfff; }
  .lp-kh-ic svg, .lp-kefu-orb svg, .lp-kefu-contact svg { fill:none; stroke:currentColor; stroke-width:1.7; stroke-linecap:round; stroke-linejoin:round; }
  .lp-kh-ic svg { width:22px; height:22px; }
  .lp-kefu-head b { display:block; font-size:16px; color:#eaf3ff; }
  .lp-kefu-head small { font-size:12.5px; color:#8a97b0; }
  .lp-kefu-close { margin-left:auto; background:none; border:none; color:#8a97b0; font-size:24px; line-height:1; cursor:pointer; padding:2px 6px; }
  .lp-kefu-close:hover { color:#eaf3ff; }
  .lp-kefu-body { padding:26px 22px 18px; text-align:center; }
  .lp-kefu-orb { width:78px; height:78px; margin:0 auto 14px; border-radius:50%; display:grid; place-items:center; color:#3ecfff; border:2px solid rgba(62,207,255,.5); box-shadow:0 0 34px -6px rgba(62,207,255,.6),inset 0 0 24px -6px rgba(62,207,255,.4); }
  .lp-kefu-orb svg { width:36px; height:36px; }
  .lp-kefu-title { font-size:18px; font-weight:700; color:#eaf3ff; }
  .lp-kefu-status { display:inline-flex; align-items:center; gap:6px; margin-top:8px; font-size:12.5px; color:#2de2a0; }
  .lp-kefu-status::before { content:""; width:7px; height:7px; border-radius:50%; background:#2de2a0; box-shadow:0 0 8px #2de2a0; }
  .lp-kefu-qr { width:180px; height:180px; margin:18px auto 8px; border-radius:12px; background:#fff; padding:12px; }
  .lp-kefu-qr img { width:100%; height:100%; object-fit:contain; }
  .lp-kefu-qr-ph { width:100%; height:100%; display:grid; place-items:center; gap:4px; text-align:center; color:#6b7a95; font-size:12px; border:2px dashed #c3d0e0; border-radius:8px; }
  .lp-kefu-qr-ph svg { width:34px; height:34px; fill:none; stroke:#9aa8bd; stroke-width:1.6; }
  .lp-kefu-hint { font-size:12.5px; color:#8a97b0; }
  .lp-kefu-contact { display:flex; align-items:center; justify-content:center; gap:8px; width:calc(100% - 44px); margin:4px 22px 22px; height:48px; border-radius:12px; border:none; cursor:pointer; font-size:15px; font-weight:650; color:#04121a; background:linear-gradient(94deg,#3ecfff,#4d9bff 60%,#8f7bff); box-shadow:0 10px 28px -8px rgba(62,207,255,.7); font-family:inherit; }
  .lp-kefu-contact svg { width:19px; height:19px; }
  @media (prefers-reduced-motion: reduce){ .lp-kefu-modal, .lp-kefu-card, .lp-start, .lp-fab { transition:none; } }
`
