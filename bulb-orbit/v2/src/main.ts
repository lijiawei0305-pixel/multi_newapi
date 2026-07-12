// ==========================================
// 临时最小启动器（TEMPORARY） —— 仅为 Task 9 粒子灯泡提供一个可见的宿主页面，供视觉门截图核对。
// Task 11 会整体重写本文件（轨道卫星、HUD、交互、后期 UnrealBloomPass 等），这里刻意保持最小。
// ==========================================
import * as THREE from 'three'
import { createBulb } from './scene/bulb'
import { CAMERA_Z } from './scene/config'

const canvas = document.getElementById('webgl-canvas')
if (!(canvas instanceof HTMLCanvasElement)) throw new Error('#webgl-canvas not found')

const scene = new THREE.Scene()

const camera = new THREE.PerspectiveCamera(45, window.innerWidth / window.innerHeight, 0.1, 100)
camera.position.set(0, 0, CAMERA_Z)

const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true })
renderer.setSize(window.innerWidth, window.innerHeight)
renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))

// 灯泡自身的 point light 内置在 bulb.group 里（供 setCoreColor 调色，createBulb() 不接收外部
// scene/light 引用）；这里只补一盏与 yun 一致的暗蓝环境光做基础补光。
const ambientLight = new THREE.AmbientLight('#040d20', 1.5)
scene.add(ambientLight)

const mouse = { x: 0, y: 0 }
let hasPointer = false

window.addEventListener('mousemove', (event: MouseEvent) => {
  mouse.x = (event.clientX / window.innerWidth) * 2 - 1
  mouse.y = -(event.clientY / window.innerHeight) * 2 + 1
  hasPointer = true
})

// 光标离开页面：停止排斥场，让粒子回流复位
document.addEventListener('mouseleave', () => {
  hasPointer = false
})

window.addEventListener('resize', () => {
  camera.aspect = window.innerWidth / window.innerHeight
  camera.updateProjectionMatrix()
  renderer.setSize(window.innerWidth, window.innerHeight)
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
})

async function main(): Promise<void> {
  const bulb = await createBulb()
  scene.add(bulb.group)

  const clock = new THREE.Clock()

  function animate(): void {
    requestAnimationFrame(animate)
    bulb.update(clock.getElapsedTime(), mouse, hasPointer, camera)
    renderer.render(scene, camera)
  }
  animate()
}

main().catch((err: unknown) => {
  console.error('Fatal bootstrap error:', err)
})
