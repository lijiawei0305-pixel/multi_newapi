// 用法：node tools/resample-bulb.mjs [<dengpao.glb 路径>]（省略则用下面 DEFAULT_GLB）
// 依赖 three 的 GLTFLoader + BufferGeometryUtils。IO/装载编排，不单测（纯采样数学见 sampler.mjs + sampler.test.mjs）。
//
// GLTFLoader 内部引用浏览器全局 `self`（纹理加载路径用到 self.URL.createObjectURL），
// 纯 Node 环境没有这个全局。ESM 的静态 import 会被提升到模块求值最前面执行，
// 如果 `import { GLTFLoader } from '...'` 写成静态 import，polyfill 语句来不及生效
// 就会先报 "self is not defined"。所以这里先挂 polyfill，再用动态 import 加载这两个
// three addon，保证求值顺序是：polyfill → GLTFLoader/BufferGeometryUtils 求值。
globalThis.self = globalThis

import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { areaWeightedSample, laplacianSmooth } from './sampler.mjs'

const { GLTFLoader } = await import('three/examples/jsm/loaders/GLTFLoader.js')
const BufferGeometryUtils = await import('three/examples/jsm/utils/BufferGeometryUtils.js')

// 源 GLB（83MB）本次会话已一次性下载到 scratchpad，不入库、不进仓库；
// 可用 argv 覆盖指向其它路径（例如以后 yun 换了新模型）。
const DEFAULT_GLB = '/private/tmp/claude-501/-Users-cc-newapi628/4025aa17-29a1-4b05-9fdc-84abc4e724c8/scratchpad/dengpao.glb'
const glbPath = process.argv[2] || DEFAULT_GLB
const outPath = 'public/models/dengpao_points_smooth.bin'

const buf = readFileSync(glbPath)
const loader = new GLTFLoader()

const gltf = await new Promise((resolve, reject) => {
  loader.parse(buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.byteLength), '', resolve, reject)
})

const geos = []
gltf.scene.traverse((o) => { if (o.isMesh) geos.push(o.geometry.toNonIndexed().clone().applyMatrix4(o.matrixWorld)) })
if (geos.length === 0) { console.error('no mesh found in', glbPath); process.exit(1) }

let geo = BufferGeometryUtils.mergeGeometries(geos)
geo = BufferGeometryUtils.mergeVertices(geo)   // 建立索引以便平滑/邻接
geo.computeVertexNormals()

const positions = geo.attributes.position.array
const indices = geo.index.array

// 注：draft 里原计划用 LoopSubdivision 细分，但 three 0.160 addons 里那个 import 路径
// 不完整/不可用，已删除；圆润效果改由 laplacianSmooth(2 次) 去棱角 + 面积加权采样达成
// （细分是可选增强，非必需——本工具走无细分路径）。
const smoothed = laplacianSmooth(positions, indices, 2)   // 2 次平滑消棱角
geo.attributes.position.array.set(smoothed); geo.computeVertexNormals()
const normals = geo.attributes.normal.array

let seed = 12345; const rand = () => (seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff
const out = areaWeightedSample(new Float32Array(smoothed), new Uint32Array(indices), new Float32Array(normals), 40000, rand)

mkdirSync('public/models', { recursive: true })
writeFileSync(outPath, Buffer.from(out.buffer, out.byteOffset, out.byteLength))
console.log('wrote', outPath, out.length / 6, 'points')
