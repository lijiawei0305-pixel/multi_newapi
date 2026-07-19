/* 悬停选中的吸附/锁定(hysteresis)纯函数 —— 由 scene3d-interaction.test.ts 单测。
   规则:光标离开视觉区即关闭;命中某图标即锁定(含切换到另一图标);命中空处则保持当前锁定不松手。
   这条"命中空处不松手"是"悬停缩小让位不抽搐"的关键:画面 A1 缩小后图标漂离光标,选中仍锁死。 */
export function nextSelection(
  current: string | null,
  hitKey: string | null,
  pointerInside: boolean
): string | null {
  if (!pointerInside) return null
  if (hitKey) return hitKey
  return current
}
