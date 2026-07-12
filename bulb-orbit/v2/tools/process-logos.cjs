// One-off asset pipeline (ported from yun's scripts/process_logos.cjs): renders the
// 24 landing-page model badges into transparent 512x512 RGBA PNGs under public/logos/.
//
// Two source kinds, normalized onto the same canvas:
//   - 12 "domestic" (国产) logos: already-transparent PNGs at bulb-orbit/logos/<id>.png.
//   - 12 "international" (国际) logos: inline SVGs extracted from the old
//     bulb-orbit/index.html LOGOS map (gradient ids de-duplicated), staged at
//     INTL_SVG_DIR below for this run.
//
// Both kinds go through: resize-contain onto a 400x400 transparent box, then extend
// 56px transparent padding on every side to reach the final 512x512 — this keeps every
// badge's visual weight uniform regardless of the source's original content-to-canvas
// ratio (raw domestic renders and freshly rasterized SVGs don't start out consistent).
//
// Display compensation: the hero renders through EffectComposer's OutputPass, which
// applies ACES filmic tone mapping to the whole frame (per-material toneMapped:false is
// ignored inside render targets). To keep on-screen brand colors matching the source
// art, we bake the exact inverse of three.js's ACESFilmicToneMapping into each texture:
// ACES(invACES(c)) == c. See aces.cjs for the pure math (unit-tested there).
'use strict'

const fs = require('fs');
const path = require('path');
const sharp = require('sharp');
const { inverseAcesFilmic, srgbToLinear, linearToSrgb } = require('./aces.cjs');

const REPO_ROOT = path.join(__dirname, '..');
const OUT_DIR = path.join(REPO_ROOT, 'public', 'logos');
const OUT_SIZE = 512;
const CONTENT_SIZE = 400;
const PAD = (OUT_SIZE - CONTENT_SIZE) / 2; // 56
const SVG_DENSITY = 384;
const TRANSPARENT = { r: 0, g: 0, b: 0, alpha: 0 };

// bulb-orbit/logos/<id>.png — already transparent, produced by a separate pipeline.
const DOMESTIC_SRC_DIR = path.join(REPO_ROOT, '..', 'logos');
const DOMESTIC_IDS = [
  'qwen', 'minimax', 'doubao', 'stepfun', 'kimi', 'huawei_pangu',
  'baidu_wenxin', 'zeroone_ai', 'tencent_hunyuan', 'baichuan_ai',
  'glm_chatglm', 'iflytek_spark',
];

// Inline SVGs extracted from the old bulb-orbit/index.html LOGOS map (gradient ids
// de-duplicated across files), committed under assets/intl-svg/ so this pipeline is
// reproducible from a clean checkout.
const INTL_SVG_DIR = path.join(__dirname, '..', 'assets', 'intl-svg');
const INTL_IDS = [
  'openai', 'anthropic', 'gemini', 'meta', 'mistral', 'deepseek',
  'xai', 'cohere', 'midjourney', 'stability', 'huggingface', 'perplexity',
];

fs.mkdirSync(OUT_DIR, { recursive: true });

// Resize-contain the source onto a transparent OUT_SIZE canvas at a uniform badge
// scale, returning a raw RGBA buffer + dims for the pixel-level compensation pass.
async function normalizeToCanvas(sharpPipeline) {
  const { data, info } = await sharpPipeline
    .ensureAlpha()
    .resize(CONTENT_SIZE, CONTENT_SIZE, { fit: 'contain', background: TRANSPARENT })
    .extend({ top: PAD, bottom: PAD, left: PAD, right: PAD, background: TRANSPARENT })
    .raw()
    .toBuffer({ resolveWithObject: true });
  return { data, width: info.width, height: info.height, channels: info.channels };
}

// Bake inverse-ACES into every non-transparent pixel so the runtime OutputPass tone
// mapping cancels back out to the original color. Alpha, and fully transparent
// pixels, pass through untouched.
function compensate(data, width, height, channels) {
  for (let i = 0; i < width * height; i++) {
    const o = i * channels;
    if (data[o + 3] === 0) continue;
    const rgb = inverseAcesFilmic([
      srgbToLinear(data[o]),
      srgbToLinear(data[o + 1]),
      srgbToLinear(data[o + 2]),
    ]);
    data[o] = linearToSrgb(rgb[0]);
    data[o + 1] = linearToSrgb(rgb[1]);
    data[o + 2] = linearToSrgb(rgb[2]);
  }
  return data;
}

async function writeOutput(id, data, width, height, channels) {
  const outFile = path.join(OUT_DIR, `${id}.png`);
  await sharp(data, { raw: { width, height, channels } }).png().toFile(outFile);
  console.log(`-> public/logos/${id}.png`);
}

async function processDomestic(id) {
  const srcFile = path.join(DOMESTIC_SRC_DIR, `${id}.png`);
  if (!fs.existsSync(srcFile)) throw new Error(`MISSING domestic source: ${srcFile}`);
  const { data, width, height, channels } = await normalizeToCanvas(sharp(srcFile));
  compensate(data, width, height, channels);
  await writeOutput(id, data, width, height, channels);
}

async function processIntl(id) {
  const srcFile = path.join(INTL_SVG_DIR, `${id}.svg`);
  if (!fs.existsSync(srcFile)) throw new Error(`MISSING intl SVG source: ${srcFile}`);
  const svgBuffer = fs.readFileSync(srcFile);
  const { data, width, height, channels } = await normalizeToCanvas(sharp(svgBuffer, { density: SVG_DENSITY }));
  compensate(data, width, height, channels);
  await writeOutput(id, data, width, height, channels);
}

async function main() {
  const errors = [];
  for (const id of DOMESTIC_IDS) {
    try { await processDomestic(id); } catch (err) { errors.push(err.message); }
  }
  for (const id of INTL_IDS) {
    try { await processIntl(id); } catch (err) { errors.push(err.message); }
  }
  const total = DOMESTIC_IDS.length + INTL_IDS.length;
  if (errors.length) {
    console.error(`${errors.length}/${total} logo(s) FAILED — refusing to silently drop:`);
    for (const e of errors) console.error(`  - ${e}`);
    process.exitCode = 1;
    return;
  }
  console.log(`Done. ${total}/${total} logos written to public/logos/.`);
}

main();
