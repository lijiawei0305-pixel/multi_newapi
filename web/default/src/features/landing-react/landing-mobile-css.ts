/* 落地页移动端样式 —— 全部规则包在媒体查询内，宽屏零生效（M1 桌面端零变化）。
   注入顺序：index.tsx 中 LANDING_CSS + SECTIONS_CSS + MOBILE_CSS（后声明者覆盖同特异性）。
   回滚：从 <style> 去掉 + MOBILE_CSS 一个 token 即完全回退移动端样式。
   断点单一真源：BREAKPOINTS（viewport-mode.ts）；全文件禁止 !important。 */
import { BREAKPOINTS } from './viewport-mode'

export const MOBILE_CSS = `
/* ============ 手机端（≤${BREAKPOINTS.mobile}px）============ */
@media (max-width: ${BREAKPOINTS.mobile}px) {

  /* --- 全局 --- */
  .wd-landing-root { -webkit-text-size-adjust:100%; --chip:40px; }
  #bg { height:100vh; height:100svh; }

  /* --- 顶栏 --- */
  .lp-topbar {
    padding:12px 16px;
    padding-top:calc(12px + env(safe-area-inset-top, 0px));
    background:rgba(3,7,18,.72);
    backdrop-filter:blur(14px); -webkit-backdrop-filter:blur(14px);
    border-bottom:1px solid rgba(120,180,255,.10);
  }
  .lp-kefu-btn { display:none; }                       /* D7：手机只留 FAB */
  .lp-logo { font-size:16px; gap:8px; }
  .lp-logo .lp-mark-img { width:34px; height:34px; }
  .lp-actions { gap:8px; }
  .lp-start { height:36px; padding:0 14px; font-size:13.5px; }
  .lp-actions [aria-haspopup="menu"] { height:36px; width:36px; }

  /* --- 首屏容器：横向分栏 → 竖向堆叠（R1/R10）--- */
  .hero-stage {
    height:auto;
    min-height:100vh;        /* 老浏览器回落 */
    min-height:100svh;       /* 支持则覆盖，消除 iOS 地址栏跳变 */
    flex-direction:column; align-items:stretch;
  }
  /* 顶栏改造后实际高 ≈60px（12px×2 内边距 + 36px 按钮），故留 64px 余量 + 16px 呼吸 */
  .hero-left {
    flex:0 0 auto;
    padding:calc(64px + env(safe-area-inset-top, 0px) + 16px) 20px 8px;
    gap:10px; justify-content:flex-start;
  }
  .hero-right { flex:1 1 auto; min-height:42vh; min-height:42svh; }
  /* 特异性陷阱：须写足复合选择器（landing-css .hero-right.card-open .hero-visual = 0,3,0） */
  .hero-right.card-open .hero-visual { transform:none; }
  #bulb { height:min(38vh, 70vw); height:min(38svh, 70vw); }

  /* --- 首屏文字（R4）--- */
  #hero { gap:10px; }
  #hero h1 { gap:4px; margin-top:2px; }
  #badge { font-size:12px; letter-spacing:.08em; padding:6px 13px; }
  .hl1 { font-size:26px; letter-spacing:.04em; padding-left:0; }
  .hl2 { font-size:34px; letter-spacing:.01em; padding-left:0; line-height:1.2; }
  #sub { font-size:14px; letter-spacing:.06em; line-height:1.7; margin-top:0; }

  /* --- 首屏两张卡片：两列并排（R2/D10）--- */
  .features { max-width:none; gap:14px 12px; margin-top:2px; }
  .feature { width:calc(50% - 6px); min-width:0; gap:9px; }   /* min-width:0 = R2 根因 */
  .feature-ico { width:30px; height:30px; border-radius:8px; }
  .feature-ico svg { width:17px; height:17px; }
  .feature-name { font-size:13.5px; }
  .feature-desc { font-size:11.5px; margin-top:2px; line-height:1.45; }

  /* --- CTA：等宽并排（R3）--- */
  .cta { gap:10px; margin-top:6px; width:100%; }
  .btn { flex:1 1 0; min-width:0; padding:12px 10px; font-size:14px; }

  /* --- HUD → 底部抽屉（R8/D6）--- */
  #hud {
    position:fixed; z-index:70; pointer-events:auto;
    left:0; right:0; bottom:0; top:auto;
    width:auto; max-width:none;
    border:1px solid rgba(120,180,255,.22);
    border-top:3px solid var(--hud-accent, #00f0ff);
    border-radius:18px 18px 0 0;
    padding:16px 18px calc(18px + env(safe-area-inset-bottom, 0px));
    transform:translateY(100%);
    transition:opacity .3s ease,
               transform .38s cubic-bezier(.22,.7,.25,1),
               visibility 0s linear .38s;
  }
  #hud.show {
    transform:translateY(0);
    transition:opacity .3s ease, transform .38s cubic-bezier(.22,.7,.25,1);
  }
  #hud .hud-desc { font-size:13px; }
  .hud-close {
    display:grid; place-items:center;
    position:absolute; top:10px; right:12px;
    width:32px; height:32px; border-radius:50%;
    background:rgba(255,255,255,.06);
    border:1px solid rgba(120,180,255,.18);
    color:#cfe0f5; font-size:20px; line-height:1;
    font-family:inherit; cursor:pointer; padding:0;
  }

  /* --- 客服 FAB（R9/D7）--- */
  .lp-fab {
    width:48px; height:48px; right:16px;
    bottom:calc(18px + env(safe-area-inset-bottom, 0px));
  }
  .lp-fab svg { width:22px; height:22px; }
  /* ① 抽屉升起时隐藏客服 FAB —— 两者都在屏底会重叠，且 FAB 紧邻 × 按钮易误点。
        .lp-fab 是 .hero-stage 的「前序兄弟」，纯 CSS 选不到 #hud.show，
        故由 onSelectChange 在 .wd-landing-root 上切 wd-hud-open 类（T05）。 */
  .wd-landing-root.wd-hud-open .lp-fab {
    opacity:0; pointer-events:none; transform:translateY(8px);
  }

  /* --- AI 工坊 --- */
  #sec-studio { padding:56px 16px 28px; }
  .svc-stu-title { font-size:27px; }
  .svc-stu-sub { font-size:14px; max-width:none; }
  .svc-cap { padding:20px; }
  .svc-cap-ic { width:40px; height:40px; margin-bottom:14px; }
  .svc-cap h3 { font-size:16.5px; }
  .svc-cap p { font-size:13px; }

  /* --- 核心能力（R6 的配套收敛；等高修复在 sections-css 的 560 断点）--- */
  #sec-services { padding:56px 16px; }
  .svc-card { padding:18px; }
  .svc-flagship { padding:22px 18px; }
  /* 特异性陷阱：须同时列 .svc-flagship .svc-card-desc（sections-css = 0,2,0） */
  .svc-card-desc,
  .svc-flagship .svc-card-desc { max-width:none; }
  .svc-lede { font-size:14px; }

  /* --- 页尾 CTA --- */
  .join-constellation { opacity:.4; }

  /* --- 可选 · 滚动流畅度（见 proposal §7.12，可整段删除）--- */
  #grid { display:none; }
  #aurora { filter:blur(28px); opacity:.22; }
}

/* ============ 窄屏微调（≤${BREAKPOINTS.narrow}px，覆盖 360–400）============ */
@media (max-width: ${BREAKPOINTS.narrow}px) {
  .hl1 { font-size:24px; }
  .hl2 { font-size:30px; }
  #sub { font-size:13px; letter-spacing:.04em; }
  .hero-left { padding-left:16px; padding-right:16px; }
  .feature-desc { font-size:11px; }
  .btn { font-size:13.5px; padding:11px 8px; }
}

/* ============ 矮屏（如 360×640）============ */
@media (max-width: ${BREAKPOINTS.mobile}px) and (max-height: ${BREAKPOINTS.shortViewport}px) {
  .hero-right { min-height:34vh; min-height:34svh; }
  .hero-left { padding-top:calc(56px + env(safe-area-inset-top, 0px) + 12px); }
  #hero { gap:8px; }
}

/* ============ 触屏：中和粘滞 hover（R11）============ */
@media (hover: none) {
  .chip:hover .shell { transform:none; }
  .chip:hover .shell::before {
    border-color:rgba(120,180,255,.6);
    box-shadow:0 0 12px rgba(80,160,255,.42), 0 0 30px rgba(50,120,255,.18),
               inset 0 0 14px rgba(90,170,255,.16), inset 0 -5px 12px rgba(0,0,0,.5);
  }
}
`
