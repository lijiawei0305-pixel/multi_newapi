const fs = require('fs');
const path = require('path');

const dengpaoPath = 'D:\\yun\\dengpao.glb';
const logoDir = 'D:\\yun\\glb';
const outputDir = 'D:\\yun\\public\\models';

const TARGET_PARTICLE_COUNT = 20000;

// Ensure output directory exists
if (!fs.existsSync(outputDir)) {
  fs.mkdirSync(outputDir, { recursive: true });
}

function rotateVectorWithQuaternion(x, y, z, q) {
  const [qx, qy, qz, qw] = q;
  const tx = 2 * (qy * z - qz * y);
  const ty = 2 * (qz * x - qx * z);
  const tz = 2 * (qx * y - qy * x);
  const rx = x + qw * tx + qy * tz - qz * ty;
  const ry = y + qw * ty + qz * tx - qx * tz;
  const rz = z + qw * tz + qx * ty - qy * tx;
  return [rx, ry, rz];
}

function extractGLBVertices(filePath, targetCount) {
  if (!fs.existsSync(filePath)) {
    console.error(`File not found: ${filePath}`);
    return null;
  }
  const buffer = fs.readFileSync(filePath);
  if (buffer.length < 20) return null;

  const chunkLength = buffer.readUInt32LE(12);
  const jsonContent = buffer.toString('utf8', 20, 20 + chunkLength);
  const json = JSON.parse(jsonContent);

  let posAccessorIdx = 0;
  try {
    posAccessorIdx = json.meshes[0].primitives[0].attributes.POSITION;
  } catch (e) {
    console.error(`Failed to find POSITION attribute in ${filePath}`, e.message);
    return null;
  }

  const accessor = json.accessors[posAccessorIdx];
  const bufferView = json.bufferViews[accessor.bufferView];

  const jsonChunkEnd = 12 + 8 + chunkLength;
  const chunk0AlignedLength = Math.ceil(chunkLength / 4) * 4;
  const chunk1HeaderOffset = 12 + 8 + chunk0AlignedLength;
  const chunk1DataOffset = chunk1HeaderOffset + 8;

  const byteOffset = chunk1DataOffset + bufferView.byteOffset + (accessor.byteOffset || 0);
  const count = accessor.count;

  const vertexData = new Float32Array(buffer.buffer, buffer.byteOffset + byteOffset, count * 3);

  let minX = Infinity, minY = Infinity, minZ = Infinity;
  let maxX = -Infinity, maxY = -Infinity, maxZ = -Infinity;

  for (let i = 0; i < count; i++) {
    const x = vertexData[i * 3];
    const y = vertexData[i * 3 + 1];
    const z = vertexData[i * 3 + 2];
    if (x < minX) minX = x;
    if (x > maxX) maxX = x;
    if (y < minY) minY = y;
    if (y > maxY) maxY = y;
    if (z < minZ) minZ = z;
    if (z > maxZ) maxZ = z;
  }

  const centerX = (minX + maxX) / 2;
  const centerY = (minY + maxY) / 2;
  const centerZ = (minZ + maxZ) / 2;

  const sizeX = maxX - minX;
  const sizeY = maxY - minY;
  const sizeZ = maxZ - minZ;
  const maxDimension = Math.max(sizeX, sizeY, sizeZ);

  const sampledData = new Float32Array(targetCount * 3);
  const node = json.nodes && json.nodes[0];
  const rotation = node && node.rotation;

  for (let i = 0; i < targetCount; i++) {
    const origIdx = Math.floor((i * count) / targetCount);

    let x = (vertexData[origIdx * 3] - centerX) / maxDimension;
    let y = (vertexData[origIdx * 3 + 1] - centerY) / maxDimension;
    let z = (vertexData[origIdx * 3 + 2] - centerZ) / maxDimension;

    if (rotation) {
      const [rx, ry, rz] = rotateVectorWithQuaternion(x, y, z, rotation);
      x = rx;
      y = ry;
      z = rz;
    }

    sampledData[i * 3] = x;
    sampledData[i * 3 + 1] = y;
    sampledData[i * 3 + 2] = z;
  }

  return sampledData;
}

// Process dengpao.glb
console.log('--- Processing dengpao ---');
const dpData = extractGLBVertices(dengpaoPath, TARGET_PARTICLE_COUNT);
if (dpData) {
  const destPath = path.join(outputDir, 'dengpao.bin');
  fs.writeFileSync(destPath, Buffer.from(dpData.buffer));
  console.log(`Saved dengpao.bin to ${destPath}`);
}

// Process logos
const logoFiles = fs.readdirSync(logoDir).filter(f => f.endsWith('.glb'));
console.log(`\n--- Processing ${logoFiles.length} logos ---`);
logoFiles.forEach(file => {
  const filePath = path.join(logoDir, file);
  const name = path.basename(file, '.glb');
  const data = extractGLBVertices(filePath, TARGET_PARTICLE_COUNT);
  if (data) {
    const destPath = path.join(outputDir, `${name}.bin`);
    fs.writeFileSync(destPath, Buffer.from(data.buffer));
    console.log(`Saved ${name}.bin to ${destPath}`);
  } else {
    console.error(`Failed to process ${file}`);
  }
});

console.log('\n--- All particles extracted successfully! ---');
