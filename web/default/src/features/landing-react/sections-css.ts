/* WeDream 落地页「三屏」样式 —— 逐字移植自 bulb-orbit/index.html 的 <style>：
     .reveal            → 原版 325–326
     #sec-services      → 原版 371–464
     #sec-studio        → 原版 466–522
     #sec-join          → 原版 524–593
   与 LANDING_CSS 一样，挂载时注入 <style>、卸载即移除（见 index.tsx），不永久污染全站。
   注：原版 319–368 的 .section 与 .agent- / .adv- / .plan- 系列是早期方案遗留的死 CSS（HTML 未使用），不移植。 */
export const SECTIONS_CSS = `
  .reveal { opacity:0; transform:translateY(28px); transition:opacity .7s ease, transform .7s cubic-bezier(.22,.7,.25,1); }
  .reveal.in { opacity:1; transform:none; }

  /* ==================== 核心能力 / core services ==================== */
  #sec-services {
    --svc-surface:rgba(255,255,255,.04);
    --svc-surface-2:rgba(255,255,255,.06);
    --svc-border:rgba(148,163,184,.15);
    --svc-border-hi:rgba(62,207,255,.42);
    --svc-ink:#e9eefb;
    --svc-ink-dim:#8a97b0;
    --svc-ink-faint:#6f7d97;
    --svc-cyan:#3ecfff;
    --svc-indigo:#8f7bff;
    --svc-grad:linear-gradient(135deg,#3ecfff 0%,#8f7bff 100%);
    --svc-mono:ui-monospace,"SF Mono","Cascadia Code",Menlo,Consolas,monospace;
    position:relative; z-index:14; overflow:hidden;
    padding:clamp(64px,8vw,110px) clamp(20px,5vw,44px);
    background:none;
  }
  .svc-wrap { width:100%; max-width:1200px; margin:0 auto; }

  .svc-head { display:grid; grid-template-columns:1.35fr 1fr; gap:24px 48px; align-items:end; margin-bottom:clamp(32px,5vw,52px); }
  .svc-eyebrow { grid-column:1 / -1; margin:0 0 4px; font-family:var(--svc-mono); font-size:12px; letter-spacing:.24em; text-transform:uppercase; color:var(--svc-ink-faint); display:flex; align-items:center; gap:10px; }
  .svc-eyebrow::before { content:""; width:6px; height:6px; border-radius:50%; background:var(--svc-cyan); box-shadow:0 0 10px 1px rgba(62,207,255,.7); }
  .svc-title { margin:0; font-size:clamp(28px,4vw,46px); font-weight:750; line-height:1.14; letter-spacing:-.01em; color:var(--svc-ink); }
  .svc-lede { margin:0 0 6px; font-size:15px; color:var(--svc-ink-dim); max-width:42ch; }

  .svc-bento { display:grid; grid-template-columns:repeat(6,1fr); grid-auto-rows:1fr; gap:14px; }
  .svc-card { position:relative; grid-column:span 2; display:flex; flex-direction:column; padding:22px; border-radius:16px; background:var(--svc-surface); border:1px solid var(--svc-border); backdrop-filter:blur(8px); -webkit-backdrop-filter:blur(8px); transition:transform .28s ease,border-color .28s ease,background .28s ease,box-shadow .28s ease; }
  .svc-card:hover { transform:translateY(-4px); background:var(--svc-surface-2); border-color:var(--svc-border-hi); box-shadow:0 18px 44px -22px rgba(62,207,255,.5),inset 0 0 0 1px rgba(62,207,255,.08); }
  .svc-card-top { display:flex; align-items:center; justify-content:space-between; margin-bottom:20px; }
  .svc-ic { width:42px; height:42px; display:grid; place-items:center; border-radius:11px; border:1px solid var(--svc-border); background:rgba(255,255,255,.02); color:var(--svc-ink-dim); transition:color .28s ease,border-color .28s ease; }
  .svc-card:hover .svc-ic { color:var(--svc-cyan); border-color:var(--svc-border-hi); }
  .svc-ic svg { width:22px; height:22px; fill:none; stroke:currentColor; stroke-width:1.6; stroke-linecap:round; stroke-linejoin:round; }
  .svc-marker { font-family:var(--svc-mono); font-size:11px; letter-spacing:.18em; color:var(--svc-ink-faint); }
  .svc-card-title { margin:0 0 8px; font-size:17px; font-weight:650; color:var(--svc-ink); }
  .svc-card-desc { margin:0; font-size:13.5px; color:var(--svc-ink-dim); line-height:1.7; max-width:44ch; }
  .svc-tags { display:flex; flex-wrap:wrap; gap:7px; margin-top:auto; padding-top:18px; }
  .svc-tag { font-family:var(--svc-mono); font-size:11px; padding:4px 9px; border-radius:999px; color:var(--svc-ink-dim); background:rgba(255,255,255,.03); border:1px solid var(--svc-border); }

  .svc-wide { grid-column:span 3; }

  .svc-flagship { grid-column:span 4; grid-row:span 2; padding:30px 32px; border-radius:20px; overflow:hidden; }
  .svc-flagship::before { content:""; position:absolute; top:0; left:0; right:0; height:1px; background:var(--svc-grad); opacity:.7; }
  .svc-flagship::after { content:""; position:absolute; width:60%; height:70%; right:-8%; top:8%; background:radial-gradient(circle at 60% 40%,rgba(62,207,255,.22),transparent 62%); filter:blur(6px); pointer-events:none; }
  .svc-flagship .svc-ic { color:var(--svc-cyan); border-color:var(--svc-border-hi); background:rgba(62,207,255,.06); box-shadow:0 0 24px -6px rgba(62,207,255,.5); }
  .svc-flagship .svc-card-title { font-size:22px; }
  .svc-flagship .svc-card-desc { font-size:14.5px; max-width:54ch; }

  .svc-constellation { position:relative; margin-top:auto; padding-top:26px; }
  .svc-net { position:absolute; inset:12px 0 auto; width:100%; height:60px; opacity:.35; pointer-events:none; }
  .svc-chips { position:relative; display:flex; flex-wrap:wrap; gap:8px; }
  .svc-chip { font-family:var(--svc-mono); font-size:12px; padding:6px 12px; border-radius:999px; color:#cfe9ff; background:rgba(62,207,255,.05); border:1px solid rgba(62,207,255,.22); box-shadow:0 0 16px -8px rgba(62,207,255,.6); }
  .svc-chip.alt { color:#dcdcff; background:rgba(143,123,255,.06); border-color:rgba(143,123,255,.26); }

  .svc-strip { display:grid; grid-template-columns:repeat(4,1fr); gap:0; margin-top:16px; border:1px solid var(--svc-border); border-radius:16px; background:var(--svc-surface); backdrop-filter:blur(8px); -webkit-backdrop-filter:blur(8px); overflow:hidden; }
  .svc-feat { display:flex; gap:13px; align-items:flex-start; padding:20px 22px; border-left:1px solid var(--svc-border); }
  .svc-feat:first-child { border-left:none; }
  .svc-fic { flex:none; width:34px; height:34px; display:grid; place-items:center; border-radius:9px; color:var(--svc-cyan); background:rgba(62,207,255,.06); border:1px solid rgba(62,207,255,.18); }
  .svc-fic svg { width:18px; height:18px; fill:none; stroke:currentColor; stroke-width:1.6; stroke-linecap:round; stroke-linejoin:round; }
  .svc-feat b { display:block; font-size:14px; font-weight:600; color:var(--svc-ink); margin-bottom:2px; }
  .svc-feat span { font-size:12.5px; color:var(--svc-ink-dim); line-height:1.5; }

  /* 入场：滚入时交错上浮（reduced-motion 下关闭，见下方 media） */
  #sec-services .svc-card { opacity:0; transform:translateY(14px); }
  #sec-services.in .svc-card { animation:svcRise .6s cubic-bezier(.2,.7,.2,1) forwards; }
  #sec-services.in .svc-card:nth-child(1){animation-delay:.02s;}
  #sec-services.in .svc-card:nth-child(2){animation-delay:.10s;}
  #sec-services.in .svc-card:nth-child(3){animation-delay:.18s;}
  #sec-services.in .svc-card:nth-child(4){animation-delay:.26s;}
  #sec-services.in .svc-card:nth-child(5){animation-delay:.34s;}
  @keyframes svcRise { to { opacity:1; transform:translateY(0); } }
  #sec-services .svc-flagship::after { animation:svcPulse 7s ease-in-out infinite; }
  @keyframes svcPulse { 0%,100%{opacity:.75;} 50%{opacity:1;} }

  @media (max-width:900px){
    .svc-head { grid-template-columns:1fr; align-items:start; }
    .svc-lede { max-width:none; }
    .svc-bento { grid-template-columns:repeat(2,1fr); }
    .svc-card { grid-column:span 1; }
    .svc-wide { grid-column:span 1; }
    .svc-flagship { grid-column:span 2; grid-row:auto; }
    .svc-strip { grid-template-columns:repeat(2,1fr); }
    .svc-feat:nth-child(3){border-left:none;}
    .svc-feat:nth-child(n+3){border-top:1px solid var(--svc-border);}
  }
  @media (max-width:560px){
    .svc-bento { grid-template-columns:1fr; }
    .svc-bento { grid-auto-rows:auto; }
    .svc-card,.svc-wide,.svc-flagship { grid-column:span 1; }
    .svc-strip { grid-template-columns:1fr; }
    .svc-feat { border-left:none; }
    .svc-feat:nth-child(n+2){border-top:1px solid var(--svc-border);}
  }
  @media (prefers-reduced-motion: reduce){
    #sec-services .svc-card { opacity:1; transform:none; animation:none; }
    #sec-services .svc-flagship::after { animation:none; }
  }

  /* ==================== AI 工坊 / studio ==================== */
  #sec-studio {
    --svc-surface:rgba(255,255,255,.04);
    --svc-surface-2:rgba(255,255,255,.06);
    --svc-border:rgba(148,163,184,.15);
    --svc-border-hi:rgba(62,207,255,.42);
    --svc-ink:#e9eefb;
    --svc-ink-dim:#8a97b0;
    --svc-ink-faint:#6f7d97;
    --svc-cyan:#3ecfff;
    --svc-indigo:#8f7bff;
    --svc-grad:linear-gradient(135deg,#3ecfff 0%,#8f7bff 100%);
    --svc-mono:ui-monospace,"SF Mono","Cascadia Code",Menlo,Consolas,monospace;
    position:relative; z-index:14; overflow:hidden;
    padding:clamp(56px,8vw,104px) clamp(20px,5vw,44px) clamp(28px,4vw,48px);
    background:none;
  }
  .svc-stu-head { max-width:780px; margin:0 auto clamp(32px,5vw,52px); text-align:center; }
  #sec-studio .svc-eyebrow { justify-content:center; }
  .svc-stu-title { margin:0 0 14px; font-size:clamp(30px,4.5vw,50px); font-weight:750; line-height:1.16; letter-spacing:-.01em; color:var(--svc-ink); }
  .svc-accent { background:var(--svc-grad); -webkit-background-clip:text; background-clip:text; color:transparent; }
  .svc-stu-sub { margin:0 auto 24px; font-size:15.5px; color:var(--svc-ink-dim); max-width:52ch; }
  .svc-cta { display:inline-flex; align-items:center; gap:8px; padding:12px 24px; border-radius:12px; font-family:inherit; font-size:14.5px; font-weight:650; color:#04121a; background:var(--svc-grad); border:none; cursor:pointer; text-decoration:none; box-shadow:0 12px 32px -12px rgba(62,207,255,.65); transition:transform .2s ease,box-shadow .2s ease; }
  .svc-cta:hover { transform:translateY(-2px); box-shadow:0 18px 42px -12px rgba(62,207,255,.85); }
  .svc-cta svg { width:17px; height:17px; fill:none; stroke:currentColor; stroke-width:2; stroke-linecap:round; stroke-linejoin:round; }
  .svc-caps { display:grid; grid-template-columns:repeat(3,1fr); gap:16px; max-width:1200px; margin:0 auto; }
  .svc-cap { position:relative; padding:26px; border-radius:16px; background:var(--svc-surface); border:1px solid var(--svc-border); backdrop-filter:blur(8px); -webkit-backdrop-filter:blur(8px); transition:transform .28s ease,border-color .28s ease,background .28s ease,box-shadow .28s ease; }
  .svc-cap:hover { transform:translateY(-4px); background:var(--svc-surface-2); border-color:var(--svc-border-hi); box-shadow:0 18px 44px -22px rgba(62,207,255,.5),inset 0 0 0 1px rgba(62,207,255,.08); }
  .svc-cap-ic { width:46px; height:46px; display:grid; place-items:center; border-radius:12px; margin-bottom:18px; border:1px solid var(--svc-border); background:rgba(255,255,255,.02); color:var(--svc-ink-dim); }
  .svc-cap-ic svg { width:24px; height:24px; fill:none; stroke:currentColor; stroke-width:1.7; stroke-linecap:round; stroke-linejoin:round; }
  .svc-cap h3 { margin:0 0 8px; font-size:18px; font-weight:650; color:var(--svc-ink); }
  .svc-cap p { margin:0; font-size:13.5px; color:var(--svc-ink-dim); line-height:1.75; }
  .svc-cap-chat .svc-cap-ic { color:#3ecfff; border-color:rgba(62,207,255,.4); background:rgba(62,207,255,.06); }
  .svc-cap-image .svc-cap-ic { color:#8f7bff; border-color:rgba(143,123,255,.4); background:rgba(143,123,255,.08); }
  .svc-cap-video .svc-cap-ic { color:#4d9bff; border-color:rgba(77,155,255,.4); background:rgba(77,155,255,.08); }
  #sec-studio .svc-cap { opacity:0; transform:translateY(14px); }
  #sec-studio.in .svc-cap { animation:svcRise .6s cubic-bezier(.2,.7,.2,1) forwards; }
  #sec-studio.in .svc-cap:nth-child(1){animation-delay:.04s;}
  #sec-studio.in .svc-cap:nth-child(2){animation-delay:.14s;}
  #sec-studio.in .svc-cap:nth-child(3){animation-delay:.24s;}
  @media (max-width:820px){ .svc-caps{ grid-template-columns:1fr; } }
  @media (prefers-reduced-motion: reduce){ #sec-studio .svc-cap { opacity:1; transform:none; animation:none; } }

  /* ---------- AI 工坊辉光：顶部柔入避免与 hero 形成接缝 ---------- */
  .svc-fx {
    position:absolute; top:clamp(48px,7vh,88px); left:0; right:0;
    height:900px; width:100%; z-index:0; pointer-events:none; opacity:.82;
    -webkit-mask-image:linear-gradient(to bottom,transparent 0,#000 16%,#000 88%,transparent 100%);
    mask-image:linear-gradient(to bottom,transparent 0,#000 16%,#000 88%,transparent 100%);
  }
  #sec-studio .svc-wrap { position:relative; z-index:1; }
  @keyframes svcFxTwinkle { 0%,100%{opacity:.85;} 50%{opacity:.25;} }
  @keyframes svcFxBreathe { 0%,100%{opacity:1;} 50%{opacity:.74;} }
  @media (prefers-reduced-motion: no-preference){
    .svc-fx .svc-tw { animation:svcFxTwinkle 5s ease-in-out infinite; }
    .svc-glowgrp { animation:svcFxBreathe 9s ease-in-out infinite; transform-origin:center; transform-box:fill-box; }
  }

  /* ==================== 页尾 加入 WeDream AI / 灵感落地 CTA ==================== */
  #sec-join {
    position:relative; z-index:14; overflow:hidden;
    min-height:clamp(380px,30vw,480px); padding:clamp(78px,9vw,118px) 24px;
    display:grid; place-items:center;
    font-family:"PingFang SC","HarmonyOS Sans SC","MiSans","Microsoft YaHei",system-ui,sans-serif;
    background:radial-gradient(ellipse 54% 72% at 50% 48%,rgba(49,137,238,.085),transparent 72%);
  }
  .join-constellation { position:absolute; inset:0; width:100%; height:100%; z-index:0; pointer-events:none; opacity:.78; }
  .join-constellation-glow { fill:none; stroke:url(#joinConstellationGrad); stroke-width:7; opacity:.18; filter:url(#joinConstellationBlur); }
  .join-constellation-line { fill:none; stroke:url(#joinConstellationGrad); stroke-width:1.4; opacity:.42; }
  .join-constellation-branch { fill:none; stroke:#63bfff; stroke-width:1; opacity:.18; }
  .join-constellation-pulse {
    fill:none; stroke:#78e8ff; stroke-width:2.4; stroke-linecap:round;
    stroke-dasharray:42 958; stroke-dashoffset:0; filter:url(#joinConstellationBlur);
  }
  .join-node-halo { fill:url(#joinNodeGlow); opacity:.42; }
  .join-node-core { fill:#9befff; opacity:.82; }
  .join-node-core.alt { fill:#9a8cff; }
  .join-spark { fill:#c6f5ff; opacity:.58; }
  .join-content { position:relative; z-index:1; width:100%; max-width:900px; text-align:center; }
  .join-title {
    margin:0; color:#f3f7ff; font-size:clamp(34px,4vw,56px); font-weight:800;
    line-height:1.14; letter-spacing:-.015em; text-shadow:0 8px 34px rgba(0,8,28,.48);
  }
  .join-title span { display:block; }
  .join-title .join-accent {
    margin-top:6px;
    background:linear-gradient(100deg,#27dcf1 0%,#29bdf3 36%,#5d9cff 68%,#8f7bff 100%);
    -webkit-background-clip:text; background-clip:text; color:transparent; -webkit-text-fill-color:transparent;
  }
  .join-copy {
    max-width:720px; margin:22px auto 0; color:rgba(166,187,218,.78);
    font-size:clamp(13px,1.2vw,15.5px); line-height:1.8; letter-spacing:.04em;
  }
  .join-actions { display:flex; justify-content:center; flex-wrap:wrap; gap:14px; margin-top:30px; }
  .join-btn {
    min-width:142px; padding:12px 24px; border-radius:999px;
    display:inline-flex; align-items:center; justify-content:center; gap:8px;
    color:#d9e8fb; font-size:14px; font-weight:650; text-decoration:none;
    border:1px solid rgba(151,185,233,.28); background:rgba(7,15,31,.44);
    backdrop-filter:blur(10px); -webkit-backdrop-filter:blur(10px);
    transition:transform .2s ease,border-color .2s ease,background .2s ease,box-shadow .2s ease;
  }
  .join-btn svg { width:17px; height:17px; fill:none; stroke:currentColor; stroke-width:1.8; stroke-linecap:round; stroke-linejoin:round; }
  .join-btn-primary {
    color:#03151c; border-color:transparent;
    background:linear-gradient(100deg,#29dcef,#3ecfff 52%,#68a7ff);
    box-shadow:0 12px 34px -14px rgba(62,207,255,.82);
  }
  .join-btn:hover { transform:translateY(-2px); border-color:rgba(143,193,255,.55); background:rgba(15,28,52,.72); }
  .join-btn-primary:hover {
    border-color:transparent; background:linear-gradient(100deg,#3be5f4,#53d5ff 52%,#7ab3ff);
    box-shadow:0 16px 40px -12px rgba(62,207,255,.92);
  }
  @keyframes joinNodePulse { 0%,100%{opacity:.26;} 50%{opacity:.55;} }
  @keyframes joinConstellationFlow { to{stroke-dashoffset:-1000;} }
  @keyframes joinSparkTwinkle { 0%,100%{opacity:.25;} 50%{opacity:.82;} }
  @media (prefers-reduced-motion: no-preference){
    .join-node-halo { animation:joinNodePulse 6s ease-in-out infinite; }
    .join-constellation-pulse { animation:joinConstellationFlow 13s linear infinite; }
    .join-spark { animation:joinSparkTwinkle 4.8s ease-in-out infinite; }
  }
  @media (max-width:520px){
    #sec-join { min-height:420px; padding:88px 22px; }
    .join-title { font-size:clamp(32px,10vw,44px); }
    .join-copy { letter-spacing:0; }
    .join-actions { width:min(100%,300px); margin-left:auto; margin-right:auto; }
    .join-btn { width:100%; }
  }
`
