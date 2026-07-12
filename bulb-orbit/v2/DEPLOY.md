# DEPLOY.md — WeDream AI 落地页部署接入

> 运维 runbook。落地页（本目录 `bulb-orbit/v2/`）构建产物是纯静态文件，本文档描述如何把它接入生产站
> `*.wedreamhub.com`。**W4**：本文档在 Mac 端编写；下述涉及服务器 / 后台的步骤（②③④）由操作者在
> 服务器 `64.90.4.114` 或后台管理界面**手动执行**，不随本次开发提交自动触发。

## 前置事实（详见根 `CLAUDE.md`「服务器与部署」）

- 唯一 newapi 生产栈 `newapi_test`（fork，`newapi_test-app` + `redis` + `mysql:8.2`），监听 `127.0.0.1:3100`。
- `api` / `www` / `tokendream`.wedreamhub.com 三域名均经宝塔 nginx 反代到 3100。
- 服务器源码根：`/root/newapi-test/`（rsync 同步，非 git 副本）。
- SSH：`ssh newapi628`（等价 `ssh -i ~/.ssh/newapi628_ed25519 -p 5522 root@64.90.4.114`）。

---

## ① 本地构建 + 同步产物到服务器

本子项目是独立的 Vite 静态站（无 Go embed 依赖），Mac 端可完整构建（`npm run typecheck/lint/test/build`
四门已在 Mac 验证全绿），因此构建在 Mac 完成、只把**构建产物** `dist/` 同步上服务器，区别于主仓库
Go 后端「构建必须在服务器」的约束：

```bash
cd /Users/cc/newapi628/bulb-orbit/v2
npm run build                                   # tsc --noEmit && vite build -> dist/
ssh newapi628 mkdir -p /root/newapi-test/landing # 首次部署执行一次
rsync -avz --delete dist/ newapi628:/root/newapi-test/landing/
```

- `dist/`（约 4MB：`index.html` + `assets/*.js|css` + `logos/*.png` + `models/*.bin` + `poster.png`）
  全部使用相对路径引用资源（`vite.config.ts` 的 `base: './'`），因此天然可挂在 `/landing/` 这类子路径下，
  不需要为部署路径额外改 `base` 重新构建。
- `--delete` 保证服务器目录与本次构建产物完全一致，不遗留上一版本的散落文件。

## ② 宝塔 nginx 加静态路由

宝塔面板（`:8889`）→ 网站 → 找到 `*.wedreamhub.com` 对应站点（当前 `api`/`www`/`tokendream` 三域名反代
配置所在站点）→ 设置 → 配置文件，在 `server { }` 块内、**反代 `location /`（`proxy_pass
http://127.0.0.1:3100`）之前**插入：

```nginx
location /landing/ {
    alias /root/newapi-test/landing/;
    try_files $uri $uri/ /landing/index.html;
}
```

- **必须放在反代 `location /` 之前**：nginx 按配置文件中出现的顺序做前缀匹配择优，若放在反代规则之后，
  `/landing/*` 请求会先被通配反代规则吃掉、一律打到 3100 后端，静态目录永远不会命中。
- `try_files` 最后一项写成 `/landing/index.html`（带 `location` 前缀的绝对路径，而非裸 `index.html`）
  是 `alias` 用法下 SPA 兜底的标准写法：nginx 找不到实体文件时会用这个 URI 做一次内部重定向，
  重新匹配回同一个 `location /landing/` 块，再按 `alias` 解析成
  `/root/newapi-test/landing/index.html`。
- 保存后用面板「配置检测 / 重载」（等价 `nginx -t && nginx -s reload`）生效，避免 `systemctl restart
  nginx` 造成不必要的连接中断。
- 验收：`curl -I https://www.wedreamhub.com/landing/` 应 200 返回落地页 `index.html`；
  `/landing/assets/...`、`/landing/logos/...`、`/landing/models/...` 等静态资源均可直接访问。

## ③ 后台接入平台首页

后台「系统设置 → 常规（General）→ 首页内容（Home Page Content）」字段填：

```
https://www.wedreamhub.com/landing/
```

保存后，平台首页（`web/default/src/features/home/index.tsx`）的 `useHomePageContent()` 会识别该值为
URL（`isUrl` 分支），以 `<iframe src=... class="h-screen w-full border-none">` 整屏嵌入本落地页。
**顶部导航保持 newapi 原生**：外层 `PublicLayout` 始终渲染 `<PublicHeader>`（与 `showMainContainer`
无关），只是内容区（`showMainContainer={false}`）让 iframe 占满而不加平台自带的容器边距，因此落地页本身
不需要、也不应该再包含任何导航栏，与当前实现一致。

## ④（可选，随主平台下次构建一并处理，本任务不改代码）「立即开始」同页跳转

现状：落地页 `index.html` 的两个 CTA（`#cta-primary` → `/console`、`#cta-docs` → `/docs`）都没有设
`target`；承载 iframe 的 `web/default/src/features/home/index.tsx:54` 当前
`sandbox='allow-forms allow-popups allow-popups-to-escape-sandbox allow-scripts'`，不含
`allow-top-navigation-by-user-activation`。

若要让「立即开始」在同一浏览器标签内跳转到真正的 `/console`（而不是被嵌套渲染在 iframe 矩形里），
需要**两处一起改，缺一不可**：

1. `web/default/src/features/home/index.tsx` 的 iframe `sandbox` 追加
   `allow-top-navigation-by-user-activation`；
2. 落地页 `index.html` 的 `#cta-primary` / `#cta-docs` 两个 `<a>` 加 `target="_top"`
   ——仅放开 sandbox 而不设 `target="_top"`，点击仍只是在 iframe 自身上下文内导航（`/console` 是同源
   绝对路径，浏览器默认按 `target="_self"` 处理），效果是把整个平台控制台「套娃」加载进 iframe 里，
   而不是真正跳出到顶层标签页。

这两处分属两个代码库 / 两条构建流水线（`web/default` 随主仓库在服务器构建部署；本落地页走上面①的
独立静态构建），因此标记为可选、随主平台下次构建一并处理，本任务范围内不动 `web/default` 或
`index.html`。**在此之前**，CTA 保持现状点击会发生上述「套娃」嵌套（不报错，但观感不佳）；
另一个零风险替代方案是直接给这两个 CTA 加 `target="_blank"`（新标签页打开控制台/文档），
代价是不是「同页」体验——两者如何取舍取决于产品对「同页 vs 新标签」的偏好，本任务不替产品做决定，
故未预先修改。

---

## 回滚

本次接入是纯静态资源 + 一条 nginx `location` + 一个后台字符串字段，无数据库迁移、无状态：

1. nginx：删除或注释 `location /landing/` 块，面板重载；
2. 后台「首页内容」清空或改回旧值；
3. 均不影响 `newapi_test` 主栈（app / redis / mysql 完全不涉及本次改动），无需回滚容器或数据库。
