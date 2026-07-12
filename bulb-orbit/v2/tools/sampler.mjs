// 面积加权表面采样：每个三角按面积占比分配点数，在三角内用重心坐标撒点，法线按顶点法线插值后单位化。
export function areaWeightedSample(positions, indices, normals, count, rand) {
  const triCount = indices.length / 3
  const areas = new Float64Array(triCount)
  let total = 0
  const ax=[0,0,0], bx=[0,0,0], cx=[0,0,0], e1=[0,0,0], e2=[0,0,0], cr=[0,0,0]
  const load = (dst, i) => { dst[0]=positions[i*3]; dst[1]=positions[i*3+1]; dst[2]=positions[i*3+2] }
  for (let t = 0; t < triCount; t++) {
    load(ax, indices[t*3]); load(bx, indices[t*3+1]); load(cx, indices[t*3+2])
    for (let k=0;k<3;k++){ e1[k]=bx[k]-ax[k]; e2[k]=cx[k]-ax[k] }
    cr[0]=e1[1]*e2[2]-e1[2]*e2[1]; cr[1]=e1[2]*e2[0]-e1[0]*e2[2]; cr[2]=e1[0]*e2[1]-e1[1]*e2[0]
    areas[t] = 0.5 * Math.hypot(cr[0], cr[1], cr[2]); total += areas[t]
  }
  // 前缀和用于按面积随机选面
  const cdf = new Float64Array(triCount); let acc = 0
  for (let t=0;t<triCount;t++){ acc += areas[t]/total; cdf[t]=acc }
  const out = new Float32Array(count * 6)
  const pickTri = (r) => { let lo=0, hi=triCount-1; while(lo<hi){ const mid=(lo+hi)>>1; if(cdf[mid]<r) lo=mid+1; else hi=mid } return lo }
  for (let i = 0; i < count; i++) {
    const t = pickTri(rand())
    const ia=indices[t*3], ib=indices[t*3+1], ic=indices[t*3+2]
    let u = rand(), v = rand(); if (u+v>1){ u=1-u; v=1-v } const w = 1-u-v
    for (let k=0;k<3;k++) out[i*6+k] = w*positions[ia*3+k] + u*positions[ib*3+k] + v*positions[ic*3+k]
    let nx = w*normals[ia*3]+u*normals[ib*3]+v*normals[ic*3]
    let ny = w*normals[ia*3+1]+u*normals[ib*3+1]+v*normals[ic*3+1]
    let nz = w*normals[ia*3+2]+u*normals[ib*3+2]+v*normals[ic*3+2]
    const len = Math.hypot(nx,ny,nz) || 1; out[i*6+3]=nx/len; out[i*6+4]=ny/len; out[i*6+5]=nz/len
  }
  return out
}

// 拉普拉斯平滑：每个顶点向其一环邻居的平均位置靠拢（uniform weights），迭代 iterations 次。
export function laplacianSmooth(positions, indices, iterations) {
  const n = positions.length / 3
  const adj = Array.from({ length: n }, () => new Set())
  for (let t = 0; t < indices.length; t += 3) {
    const a=indices[t], b=indices[t+1], c=indices[t+2]
    adj[a].add(b); adj[a].add(c); adj[b].add(a); adj[b].add(c); adj[c].add(a); adj[c].add(b)
  }
  let cur = Float32Array.from(positions)
  for (let it = 0; it < iterations; it++) {
    const next = Float32Array.from(cur)
    for (let v = 0; v < n; v++) {
      if (adj[v].size === 0) continue
      let sx=0, sy=0, sz=0
      for (const w of adj[v]) { sx+=cur[w*3]; sy+=cur[w*3+1]; sz+=cur[w*3+2] }
      const inv = 1 / adj[v].size
      next[v*3] = sx*inv; next[v*3+1] = sy*inv; next[v*3+2] = sz*inv
    }
    cur = next
  }
  return cur
}
