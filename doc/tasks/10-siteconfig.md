# 🎨 SiteConfig 装修配置 — 最小可执行任务（MET）

> **职责**：`tenant_site_configs` 读写、主题色板白名单、模块开关、图片上传安全。**一期只做数据层 + 基础配置**，二期接前端（字段已全建）。
> **依赖**：`SiteConfigRepo`、对象存储 `Blob`｜ **被依赖**：代理站前端渲染（二期）、Admin 审核
> **设计参考**：[detailed-design.md](../detailed-design.md) §2.10 ｜ [proposal.md](../proposal.md) §9/§11/§13.6
> **二期预留**：字段全建（theme/template/hero/banner_json/home_mode/custom_html+status/enabled_modules），一期只渲染 `default`。

## A. 数据模型与迁移
- [ ] `tenant_site_configs` 全字段迁移（含二期预留字段）｜ ✅ 迁移跑通
- [ ] 主站默认配置 + 预设色板（8–12 色）落表/配置 ｜ ✅ 可读取

## B. 接口与逻辑
- [ ] `port.go`：`SiteConfigService`、`AssetService` ｜ ✅ `go build` 通过
- [ ] `Get`（未配置回退主站默认值）｜ ✅ 单测：回退逻辑正确
- [ ] `Patch`（主题色限色板；`home_mode` 一期仅 default/config；custom_html 锁定）｜ ✅ 单测：越界值 `THEME_NOT_IN_PALETTE/HOME_MODE_LOCKED`
- [ ] `Upload`（限 jpg/png/webp、≤2MB、绑 tenant_id、自动重命名）｜ ✅ 表驱动单测：SVG/HTML/超大 被拒 `ASSET_TYPE_FORBIDDEN/ASSET_TOO_LARGE`
- [ ] `Takedown`（管理员下架违规图片）｜ ✅ 单测：下架后不可访问

## C. 服务与 API
- [ ] 代理站基础设置接口（站点名/Logo/Favicon/Hero/标题/公告/客服/页脚）｜ ✅ 接口测：保存生效
- [ ] 主题色板选项接口 `GET /api/tenant/site-config/theme-options` ｜ ✅ 返回色板

## D. 验收
- [ ] 一期前端按 config 读取（即使是 new-api 默认皮肤）｜ ✅ 改 config 后页面取值变化
- [ ] 上传安全限制生效 ｜ ✅ 非法文件被拒
