// 依赖安全审计门禁(bun audit / npm audit 统一包装)。
//
// 为什么存在:2026-07-25 CI 事故——brace-expansion 新公告(GHSA-mh99-v99m-4gvg)
// 无兼容补丁可用(唯一补丁 5.0.8 改了 CJS 出口形状,minimatch<10 函数式调用会崩),
// 裸 `bun audit`/`npm audit` 只能全红。本包装器允许对"上游无修复路径"的具体公告
// 做**显式、留痕、带到期日**的豁免,其余任何发现照常阻断。
//
// 豁免规则(scripts/audit-allowlist.json):
//   - 按 scope(web/orbit/electron)列出 ghsa + 理由 + added + expires;
//   - 到期后同一公告重新阻断(强制复审,不允许无限期豁免);
//   - 清单条目未命中任何发现时打印提示(上游修复后应删除条目)。
// 解析失败/工具报错一律 fail-closed(退出非零),绝不静默放行。
//
// 用法: node scripts/audit-gate.mjs --tool bun|npm --dir <相对目录> --scope <名字> [--level low|moderate|high|critical]

import { spawnSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repoRoot = path.dirname(path.dirname(fileURLToPath(import.meta.url)))

const args = {}
for (let i = 2; i < process.argv.length; i += 2) {
  const key = process.argv[i]
  const value = process.argv[i + 1]
  if (!key?.startsWith('--') || value === undefined) {
    fail(`参数不成对: ${key ?? ''} ${value ?? ''}`)
  }
  args[key.slice(2)] = value
}

const tool = args.tool
const dir = args.dir
const scope = args.scope
const level = args.level ?? 'low'

if (!['bun', 'npm'].includes(tool)) fail(`--tool 只支持 bun/npm,收到: ${tool}`)
if (!dir) fail('缺少 --dir')
if (!scope) fail('缺少 --scope')

const SEVERITY_RANK = { info: 0, low: 1, moderate: 2, high: 3, critical: 4 }
const levelRank = SEVERITY_RANK[level]
if (levelRank === undefined) fail(`--level 非法: ${level}`)

function fail(message) {
  console.error(`[audit-gate:${scope ?? '?'}] ${message}`)
  process.exit(1)
}

function rank(severity) {
  // 未知等级按最高处理(fail-closed)。
  return SEVERITY_RANK[severity] ?? SEVERITY_RANK.critical
}

// ---- 读豁免清单 ----
let allowlist = []
const allowlistPath = path.join(repoRoot, 'scripts', 'audit-allowlist.json')
try {
  const parsed = JSON.parse(readFileSync(allowlistPath, 'utf8'))
  allowlist = parsed[scope] ?? []
  if (!Array.isArray(allowlist)) fail(`豁免清单 ${scope} 段不是数组`)
} catch (err) {
  fail(`无法读取豁免清单 ${allowlistPath}: ${err.message}`)
}
for (const entry of allowlist) {
  if (!entry.ghsa || !entry.reason || !entry.expires) {
    fail(`豁免条目缺字段(需 ghsa/reason/expires): ${JSON.stringify(entry)}`)
  }
}

// ---- 跑审计工具 ----
const cwd = path.join(repoRoot, dir)
const cmdArgs =
  tool === 'bun' ? ['audit', '--json'] : ['audit', '--json', '--package-lock-only']
const res = spawnSync(tool, cmdArgs, { cwd, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 })
if (res.error) fail(`无法执行 ${tool}: ${res.error.message}`)
const stdout = res.stdout ?? ''

// bun 在 JSON 前打一行人类头部;两个工具干净时都可能给空对象。
const jsonStart = stdout.indexOf('{')
if (jsonStart === -1) {
  if (res.status === 0) {
    console.log(`[audit-gate:${scope}] 无发现,通过`)
    process.exit(0)
  }
  fail(`${tool} audit 退出码 ${res.status} 且无 JSON 输出(网络/registry 故障?):\n${stdout}\n${res.stderr ?? ''}`)
}

let report
try {
  report = JSON.parse(stdout.slice(jsonStart))
} catch (err) {
  fail(`解析 ${tool} audit JSON 失败: ${err.message}`)
}
if (report.error) {
  fail(`${tool} audit 报错: ${report.error.summary ?? JSON.stringify(report.error)}`)
}

// ---- 归并出"根公告"集合(级联受害包不重复计) ----
const roots = new Map() // ghsa -> {ghsa, severity, packages:Set, title}
function addRoot(url, severity, pkg, title) {
  const ghsa = String(url ?? '').split('/').pop()
  if (!ghsa) return
  const existing = roots.get(ghsa) ?? { ghsa, severity, packages: new Set(), title }
  if (rank(severity) > rank(existing.severity)) existing.severity = severity
  existing.packages.add(pkg)
  roots.set(ghsa, existing)
}

if (tool === 'npm') {
  for (const [name, vuln] of Object.entries(report.vulnerabilities ?? {})) {
    for (const via of vuln.via ?? []) {
      if (typeof via === 'object' && via.url) {
        addRoot(via.url, via.severity, via.name ?? name, via.title)
      }
    }
  }
} else {
  for (const [pkg, advisories] of Object.entries(report)) {
    if (!Array.isArray(advisories)) continue
    for (const adv of advisories) {
      addRoot(adv.url, adv.severity, pkg, adv.title)
    }
  }
}

// ---- 判定 ----
const today = new Date().toISOString().slice(0, 10)
const blocking = []
const waived = []
const usedWaivers = new Set()

for (const root of roots.values()) {
  if (rank(root.severity) < levelRank) continue
  const waiver = allowlist.find((entry) => entry.ghsa === root.ghsa)
  if (waiver && today <= waiver.expires) {
    waived.push({ root, waiver })
    usedWaivers.add(waiver.ghsa)
  } else if (waiver) {
    blocking.push({ root, expired: waiver })
  } else {
    blocking.push({ root })
  }
}

for (const { root, waiver } of waived) {
  console.log(
    `[audit-gate:${scope}] 豁免 ${root.ghsa} (${root.severity}, ${[...root.packages].join(',')}) 至 ${waiver.expires} — ${waiver.reason}`,
  )
}
for (const entry of allowlist) {
  if (!usedWaivers.has(entry.ghsa)) {
    console.log(
      `[audit-gate:${scope}] 提示: 豁免条目 ${entry.ghsa} 未命中任何发现,上游可能已修复,请删除该条目`,
    )
  }
}

if (blocking.length > 0) {
  for (const { root, expired } of blocking) {
    const suffix = expired
      ? ` [豁免已于 ${expired.expires} 到期,需复审续期或真修]`
      : ''
    console.error(
      `[audit-gate:${scope}] 阻断 ${root.ghsa} (${root.severity}) ${[...root.packages].join(',')} — ${root.title ?? ''}${suffix}`,
    )
  }
  fail(`存在 ${blocking.length} 条未豁免的 ≥${level} 公告`)
}

console.log(`[audit-gate:${scope}] 通过(阻断 0,豁免 ${waived.length})`)
