import * as THREE from 'three';
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js';
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js';
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js';
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js';
import gsap from 'gsap';

// ==========================================
// 1. Shaders Definition (Stable contours)
// ==========================================

const vertexShader = `
  uniform float uTime;
  uniform float uSize;
  uniform vec3 uMouseOrigin;   // repulsion field ray origin (camera position)
  uniform vec3 uMouseDir;      // smoothed, normalized cursor ray direction
  uniform float uRepelStrength; // eased 0..1 (fades in on enter, out on leave)
  uniform float uRepelRadius;   // field radius around the cursor ray, world units
  uniform float uRepelPower;    // max outward displacement, world units

  attribute vec3 aNormal;
  attribute float aRandom;

  varying vec3 vColor;

  void main() {
    // 1. Gentle breathing displacement along normals (very micro, 0.4% max)
    float breathingOffset = sin(uTime * 1.1 + aRandom * 6.28318) * 0.006;

    // 2. Tiny natural noise jitter (0.25% max, for organic cell feeling)
    vec3 noiseOffset = vec3(
      sin(uTime * 0.5 + aRandom * 25.0),
      cos(uTime * 0.7 + aRandom * 30.0),
      sin(uTime * 0.9 + aRandom * 35.0)
    ) * 0.002;

    vec3 finalPos = position + aNormal * breathingOffset + noiseOffset;

    // 3. Global breathing scale pulse
    float breath = 1.0 + sin(uTime * 0.7) * 0.012;
    finalPos *= breath;

    // 4. Fluid mouse repulsion: displacement points away from the cursor ray with a
    // gaussian falloff — strongest at the cursor, so cleared particles pile up into
    // a soft ring around it. Strength/direction are eased in JS, so particles
    // disperse fluidly on approach and converge back home when the cursor leaves.
    vec3 worldPos = (modelMatrix * vec4(finalPos, 1.0)).xyz;
    vec3 toParticle = worldPos - uMouseOrigin;
    vec3 closest = uMouseOrigin + uMouseDir * dot(toParticle, uMouseDir);
    vec3 away = worldPos - closest;
    float rayDist = length(away);
    float force = uRepelStrength * exp(-(rayDist * rayDist) / (uRepelRadius * uRepelRadius));
    vec3 pushDir = rayDist > 0.0001 ? away / rayDist : aNormal;
    // per-particle stagger keeps the ring edge organic instead of mechanical
    worldPos += pushDir * force * uRepelPower * (0.75 + 0.5 * aRandom);

    // 5. Color gradient: core bright white-blue, edges deep cyan-blue;
    // displaced particles get a subtle excitation tint
    float radialDist = length(position.xz);
    vec3 coreColor = vec3(0.85, 0.94, 1.0); // Bright core
    vec3 edgeColor = vec3(0.0, 0.65, 1.0); // Cyan-blue edge
    vColor = mix(coreColor, edgeColor, smoothstep(0.06, 0.35, radialDist));
    vColor = mix(vColor, vec3(0.75, 0.95, 1.0), force * 0.6);

    // Position transform (world space, since repulsion happens there)
    vec4 mvPosition = viewMatrix * vec4(worldPos, 1.0);
    gl_Position = projectionMatrix * mvPosition;

    // 6. Individual twinkling sizing
    float twinkle = 0.7 + 0.3 * sin(uTime * (1.6 + aRandom * 2.2) + aRandom * 6.28318);
    gl_PointSize = uSize * (300.0 / -mvPosition.z) * twinkle;
  }
`;

const fragmentShader = `
  varying vec3 vColor;

  void main() {
    float dist = length(gl_PointCoord - vec2(0.5));
    if (dist > 0.5) discard;

    // Soft radial edge
    float alpha = smoothstep(0.5, 0.06, dist);

    // Bright hot center
    float core = smoothstep(0.12, 0.0, dist) * 0.6;

    vec3 finalColor = mix(vColor, vec3(1.0), core * 0.4);
    gl_FragColor = vec4(finalColor, (alpha * 0.45 + core * 0.15) * 0.75);
  }
`;

// Orbit energy-line shaders: view-depth fade (front arcs brighter, far arcs dimmed)
// plus an elliptical view-space mask that hides the far-side arc where it would
// otherwise draw a hard line across the bulb glass / filament / base.
const orbitVertexShader = `
  varying vec3 vViewPos;
  varying vec2 vUv;
  void main() {
    vUv = uv;
    vec4 mvPosition = modelViewMatrix * vec4(position, 1.0);
    vViewPos = mvPosition.xyz;
    gl_Position = projectionMatrix * mvPosition;
  }
`;

const orbitFragmentShader = `
  uniform vec3 uColor;
  uniform float uOpacity;
  uniform float uTime;
  uniform float uDir;          // orbit rotation direction: +1 or -1
  uniform float uSatCount;
  uniform float uSatAngles[8]; // current angular positions of this orbit's satellites
  uniform float uWakeStrength;
  uniform float uWakeFalloff;
  uniform vec3 uBulbView;      // bulb center in view space
  uniform vec2 uMaskRadius;    // bulb silhouette half-extents at bulb depth (view units)
  varying vec3 vViewPos;
  varying vec2 vUv;

  const float TWO_PI = 6.28318530718;

  void main() {
    // Depth fade: near arcs full strength, far arcs dimmed to ~30%
    float depthFactor = smoothstep(-5.9, -3.4, vViewPos.z);
    float alpha = uOpacity * mix(0.3, 1.0, depthFactor);

    // Bulb mask: project this fragment onto the bulb's depth plane; fragments that
    // sit behind the bulb AND inside its silhouette ellipse fade out smoothly
    vec2 atBulbDepth = vViewPos.xy * (uBulbView.z / vViewPos.z);
    float m = length((atBulbDepth - uBulbView.xy) / uMaskRadius);
    float behind = smoothstep(0.3, -0.3, vViewPos.z - uBulbView.z);
    alpha *= mix(1.0, smoothstep(0.7, 1.12, m), behind);

    // Comet wakes: the line glows where a satellite just passed and the energy decays
    // with angular distance behind it — sharp at the satellite, trailing off after.
    // (uDir flips "behind" for the counter-rotating orbit; wrap keeps it seam-safe.)
    float theta = vUv.x * TWO_PI;
    float wake = 0.0;
    for (int i = 0; i < 8; i++) {
      if (float(i) >= uSatCount) break;
      float d = (uSatAngles[i] - theta) * uDir;
      d -= TWO_PI * floor(d / TWO_PI);
      wake += exp(-d * uWakeFalloff);
    }
    wake *= uWakeStrength;

    // Subtle energy flow along the line, moving WITH the orbit's rotation direction
    // (3 wavelengths, seamless on the closed loop)
    float flow = 1.0 + 0.15 * sin(vUv.x * 18.849556 - uTime * 0.7 * uDir);

    vec3 color = uColor * (0.85 + 0.45 * depthFactor) * flow * (1.0 + wake * 1.2);
    gl_FragColor = vec4(color, min(alpha * (1.0 + wake * 1.5), 1.0));
  }
`;

// ==========================================
// 2. Global State & Configurations
// ==========================================

let scene, camera, renderer, controls, composer;
let particleGeometry, particleMaterial, particleSystem;
let pointLight, coreMesh, glowSprite, bulbGroup;
let hoveredGroup = null;

const satellites = [];
const orbitMaterials = [];

const brandColors = {
  dengpao: '#00f0ff',
  openai: '#10d075',
  claude: '#ff6b4a',
  gemini: '#9020f0',
  qwen: '#7000ff',
  doubao: '#ff2288',
  google: '#0072ff',
  grok: '#00a0ff',
  anthropic: '#7fb0e0',
  chatglm: '#2f6bff',
  ollama: '#00ffd2',
  jimeng: '#00a8ff'
};

// Pre-sampled point-cloud bin file for each logo (public/models/<bin>.bin)
const BIN_NAMES = {
  openai: 'openai',
  claude: 'claude-color',
  gemini: 'gemini-color',
  qwen: 'qwen-color',
  doubao: 'doubao-color',
  google: 'google-color',
  grok: 'grok',
  anthropic: 'anthropic',
  chatglm: 'chatglm-color',
  ollama: 'ollama',
  jimeng: 'jimeng-color'
};

const uiElements = {
  hudPanel: document.getElementById('hud-panel'),
  hudName: document.getElementById('hud-model-name'),
  hudProvider: document.getElementById('hud-model-provider'),
  hudDesc: document.getElementById('hud-model-desc'),
  hudTelemetry: document.getElementById('hud-model-telemetry')
};

const modelDetails = {
  dengpao: {
    name: 'Wedream Core',
    provider: 'SYSTEM ACTIVE',
    desc: 'Central bioluminescent intelligence hub. Orbiting satellites represent connected foundation models, communicating in real-time.',
    telemetry: '100% Core Load'
  },
  openai: {
    name: 'GPT-4o',
    provider: 'OPENAI',
    desc: 'Frontier multimodal model. Advanced logic, native audio, vision processing, and outstanding general intelligence.',
    telemetry: '99.98% Latency Sync'
  },
  claude: {
    name: 'Claude 3.5 Sonnet',
    provider: 'ANTHROPIC',
    desc: 'State-of-the-art coding and reasoning agent. Renowned for semantic accuracy, long-context scanning, and safe code execution.',
    telemetry: '200K Context Active'
  },
  gemini: {
    name: 'Gemini 1.5 Pro',
    provider: 'GOOGLE DEEPMIND',
    desc: 'Native multimodal engine with up to 2 million tokens of context window. Processes video, audio, and large repositories.',
    telemetry: '2M Token Pipeline'
  },
  qwen: {
    name: 'Qwen 2.5',
    provider: 'ALIBABA CLOUD',
    desc: 'State-of-the-art coding and multilingual model. Specialized in structured math analysis, JSON schema, and agent logic.',
    telemetry: 'Coding Expert Active'
  },
  doubao: {
    name: 'Doubao Pro',
    provider: 'BYTEDANCE',
    desc: 'High-concurrency dialogue engine. Powering massive-scale user engagement, translation pipelines, and cost-efficient tasks.',
    telemetry: 'Volcengine Pipeline'
  },
  google: {
    name: 'Gemma 2',
    provider: 'GOOGLE AI',
    desc: 'High-performance open weights model. Leverages advanced attention mechanisms and distillation for light, fast deployment.',
    telemetry: '9B Model Online'
  },
  grok: {
    name: 'Grok 2',
    provider: 'XAI',
    desc: 'Real-time social data analysis and news synthesis. Connected to the global xAI network for live world-state updates.',
    telemetry: 'Real-time Search Sync'
  },
  anthropic: {
    name: 'Anthropic API',
    provider: 'ANTHROPIC',
    desc: 'Direct access to the Claude model family with tool use, artifacts, and enterprise-grade safety controls.',
    telemetry: 'API Mesh Linked'
  },
  chatglm: {
    name: 'ChatGLM 4',
    provider: 'ZHIPU AI',
    desc: 'Bilingual foundation model with strong Chinese-language reasoning, long-context chat and agentic tool calling.',
    telemetry: 'GLM Pipeline Active'
  },
  ollama: {
    name: 'Ollama Runtime',
    provider: 'LOCAL ENGINE',
    desc: 'Developer tool for offline LLM orchestration. Runs model weights inside local system containers with minimal resource overhead.',
    telemetry: 'Local Inference active'
  },
  jimeng: {
    name: 'Jimeng AI',
    provider: 'BYTEDANCE',
    desc: 'Generative image and video engine. Turns prompts into cinematic visuals, storyboards and motion assets.',
    telemetry: 'Render Farm Online'
  }
};

// ==========================================
// 3. Helper Canvas Draw Functions
// ==========================================

// Creates a soft radial glow sprite texture for volumetric lamp core
function createGlowSpriteTexture(colorHex) {
  const canvas = document.createElement('canvas');
  canvas.width = 64;
  canvas.height = 64;
  const ctx = canvas.getContext('2d');

  const grad = ctx.createRadialGradient(32, 32, 0, 32, 32, 32);
  grad.addColorStop(0, colorHex);
  grad.addColorStop(0.2, colorHex);
  grad.addColorStop(1, 'rgba(0, 0, 0, 0)');

  ctx.fillStyle = grad;
  ctx.fillRect(0, 0, 64, 64);

  const texture = new THREE.CanvasTexture(canvas);
  return texture;
}

// ==========================================
// 4. Initial Scene Setup
// ==========================================

async function init() {
  const container = document.getElementById('webgl-canvas');

  // Scene & Fog
  scene = new THREE.Scene();
  scene.fog = new THREE.FogExp2('#01040a', 0.22);

  // Camera (Center-focused framing)
  camera = new THREE.PerspectiveCamera(45, window.innerWidth / window.innerHeight, 0.1, 100);
  camera.position.set(0, 0, 4.6);

  // Renderer
  renderer = new THREE.WebGLRenderer({ canvas: container, antialias: true, alpha: true });
  renderer.setSize(window.innerWidth, window.innerHeight);
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.0;

  // Controls
  controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;
  controls.dampingFactor = 0.05;
  controls.enableZoom = false;
  controls.minPolarAngle = Math.PI / 2.3;
  controls.maxPolarAngle = Math.PI / 1.7;

  // Lights
  const ambientLight = new THREE.AmbientLight('#040d20', 1.5);
  scene.add(ambientLight);

  pointLight = new THREE.PointLight('#00f0ff', 1.5, 8); // Lowered intensity slightly to prevent blowout
  pointLight.position.set(0, 0.14, 1.2);
  scene.add(pointLight);

  // Load stable coordinate buffer (includes position & normals)
  const response = await fetch('/models/dengpao_points.bin');
  if (!response.ok) throw new Error('Failed to load dengpao_points.bin');
  const arrayBuffer = await response.arrayBuffer();
  const floatArray = new Float32Array(arrayBuffer);

  const positions = new Float32Array(40000 * 3);
  const normals = new Float32Array(40000 * 3);
  const randoms = new Float32Array(40000);

  for (let i = 0; i < 40000; i++) {
    positions[i * 3] = floatArray[i * 6];
    positions[i * 3 + 1] = floatArray[i * 6 + 1];
    positions[i * 3 + 2] = floatArray[i * 6 + 2];

    normals[i * 3] = floatArray[i * 6 + 3];
    normals[i * 3 + 1] = floatArray[i * 6 + 4];
    normals[i * 3 + 2] = floatArray[i * 6 + 5];

    randoms[i] = Math.random();
  }

  particleGeometry = new THREE.BufferGeometry();
  particleGeometry.setAttribute('position', new THREE.BufferAttribute(positions, 3));
  particleGeometry.setAttribute('aNormal', new THREE.BufferAttribute(normals, 3));
  particleGeometry.setAttribute('aRandom', new THREE.BufferAttribute(randoms, 1));

  // Shaders (Fine 0.045 particle sizing)
  particleMaterial = new THREE.ShaderMaterial({
    vertexShader,
    fragmentShader,
    uniforms: {
      uTime: { value: 0.0 },
      uSize: { value: 0.045 },
      uMouseOrigin: { value: new THREE.Vector3(0, 0, 99) },
      uMouseDir: { value: new THREE.Vector3(0, 0, -1) },
      uRepelStrength: { value: 0.0 },
      uRepelRadius: { value: 0.26 },
      uRepelPower: { value: 0.16 }
    },
    transparent: true,
    depthWrite: false,
    blending: THREE.AdditiveBlending
  });

  particleSystem = new THREE.Points(particleGeometry, particleMaterial);
  scene.add(particleSystem);

  // Construct procedural glass shell and metal base to envelope point cloud
  setupGlassCore();

  // Create four orbital structures (crossed atomic formation)
  await setupOrbits();

  // Setup Post-processing UnrealBloomPass (Muted settings to prevent blowout)
  setupPostProcessing();

  // Setup Listeners
  setupInteractions();

  // Loop
  animate();
}

function setupPostProcessing() {
  const renderPass = new RenderPass(scene, camera);

  // Muted bloom properties to prevent pure white exposure blocks
  const bloomPass = new UnrealBloomPass(
    new THREE.Vector2(window.innerWidth, window.innerHeight),
    0.6,   // bloom strength (reduced from 0.8)
    0.45,  // radius
    0.85   // high threshold to avoid blowout
  );

  composer = new EffectComposer(renderer);
  composer.addPass(renderPass);
  composer.addPass(bloomPass);

  const outputPass = new OutputPass();
  composer.addPass(outputPass);
}

// Procedural Overlay elements for Glass Bulb & Metallic Cap
function setupGlassCore() {
  bulbGroup = new THREE.Group();
  scene.add(bulbGroup);

  // Subtle glass material (depthWrite: false prevents blocking particles inside!)
  const glassMat = new THREE.MeshPhongMaterial({
    color: 0x8be5ff,
    transparent: true,
    opacity: 0.05, // Lowered slightly
    shininess: 120,
    specular: 0xffffff,
    side: THREE.DoubleSide,
    depthWrite: false
  });

  // Dome shape
  const domeGeom = new THREE.SphereGeometry(0.36, 40, 40);
  const dome = new THREE.Mesh(domeGeom, glassMat);
  dome.position.y = 0.14;
  bulbGroup.add(dome);

  // Neck shape
  const neckGeom = new THREE.CylinderGeometry(0.36, 0.20, 0.32, 32, 1, true);
  const neck = new THREE.Mesh(neckGeom, glassMat);
  neck.position.y = -0.16;
  bulbGroup.add(neck);

  // Cap base
  const baseGeom = new THREE.CylinderGeometry(0.18, 0.18, 0.18, 32);
  const baseMat = new THREE.MeshStandardMaterial({
    color: 0x1a2b42,
    metalness: 0.9,
    roughness: 0.2,
    transparent: true,
    opacity: 0.65
  });
  const cap = new THREE.Mesh(baseGeom, baseMat);
  cap.position.y = -0.41;
  bulbGroup.add(cap);

  // Replaced solid sphere coreMesh with a volumetric glow sprite (extremely soft volumetric glow)
  const glowTexture = createGlowSpriteTexture('#00f0ff');
  const glowSpriteMat = new THREE.SpriteMaterial({
    map: glowTexture,
    transparent: true,
    opacity: 0.28,
    blending: THREE.AdditiveBlending
  });

  glowSprite = new THREE.Sprite(glowSpriteMat);
  glowSprite.scale.set(0.65, 0.65, 1.0); // Soft, diffuse size
  glowSprite.position.y = 0.14;
  bulbGroup.add(glowSprite);

  // Mini filament core wire (tiny cylinder inside)
  const coreGeom = new THREE.CylinderGeometry(0.008, 0.008, 0.12, 16);
  const coreMat = new THREE.MeshBasicMaterial({
    color: 0xffffff,
    transparent: true,
    opacity: 0.65
  });
  coreMesh = new THREE.Mesh(coreGeom, coreMat);
  coreMesh.position.y = 0.14;
  bulbGroup.add(coreMesh);
}

// Two crossed elliptical energy orbits around the bulb (bulb width = 0.72 world units):
//   primary   — front layer: low flat horizontal ellipse, brighter, ~2.8x bulb width,
//               carries the 5 core model logos
//   secondary — back layer: taller counter-rolled ellipse, thinner and dimmer,
//               ~3.6x bulb width (≈42% of viewport width), carries 6 supplementary logos
// The rolls differ in sign so the two lines cross beside the bulb, never at its center.
async function setupOrbits() {
  const ORBITS = [
    {
      radius: 1.0,
      tilt: new THREE.Euler(0.5, 0, 0.12),
      tubeRadius: 0.0028, // ≈1.3px line at the hero view distance
      color: '#8fd8ff',
      opacity: 0.55,
      speed: 0.1, // rad/s, shared by all logos on the orbit so spacing never drifts
      phase: 0,
      wakeStrength: 0.9, // comet-wake brightness behind each satellite
      wakeFalloff: 5.0, // wake decay rate per radian (lower = longer tail)
      logos: ['openai', 'claude', 'gemini', 'qwen', 'doubao']
    },
    {
      radius: 1.28,
      tilt: new THREE.Euler(0.72, 0, -0.38),
      tubeRadius: 0.0018, // ≈0.85px line
      color: '#5f7cff',
      opacity: 0.3,
      speed: -0.075, // slower counter-rotation
      phase: Math.PI / 6,
      wakeStrength: 0.6, // back layer stays more restrained
      wakeFalloff: 6.0,
      logos: ['google', 'grok', 'anthropic', 'chatglm', 'ollama', 'jimeng']
    }
  ];

  class OrbitCurve extends THREE.Curve {
    constructor(radius) {
      super();
      this.radius = radius;
    }
    getPoint(t, target = new THREE.Vector3()) {
      const theta = t * Math.PI * 2;
      return target.set(this.radius * Math.cos(theta), 0, this.radius * Math.sin(theta));
    }
  }

  // Fine glowing energy line, built from two layers sharing the same shader:
  //   core — hair-thin crisp tube (NOT a plastic ring)
  //   glow — 4x wider, very faint halo underneath, gives the line a luminous body
  // uBulbView / uTime / uSatAngles are fed per-frame in animate().
  const createOrbit = (cfg) => {
    const group = new THREE.Group();
    group.rotation.copy(cfg.tilt);
    scene.add(group);

    const makeLayer = (radiusScale, opacity) => {
      const geometry = new THREE.TubeGeometry(
        new OrbitCurve(cfg.radius), 256, cfg.tubeRadius * radiusScale, 6, true
      );
      const material = new THREE.ShaderMaterial({
        vertexShader: orbitVertexShader,
        fragmentShader: orbitFragmentShader,
        uniforms: {
          uColor: { value: new THREE.Color(cfg.color) },
          uOpacity: { value: opacity },
          uTime: { value: 0 },
          uDir: { value: Math.sign(cfg.speed) || 1 },
          uSatCount: { value: cfg.logos.length },
          uSatAngles: { value: new Array(8).fill(0) },
          uWakeStrength: { value: cfg.wakeStrength },
          uWakeFalloff: { value: cfg.wakeFalloff },
          uBulbView: { value: new THREE.Vector3() },
          // Bulb silhouette half-extents: dome ±0.36 wide, dome top +0.50 / cap bottom −0.50
          uMaskRadius: { value: new THREE.Vector2(0.48, 0.7) }
        },
        transparent: true,
        depthWrite: false,
        blending: THREE.AdditiveBlending
      });
      orbitMaterials.push({ material, cfg });

      const tube = new THREE.Mesh(geometry, material);
      tube.raycast = () => {}; // scenery, never a hover/click target
      group.add(tube);
    };

    makeLayer(1.0, cfg.opacity); // crisp core line
    makeLayer(4.0, cfg.opacity * 0.22); // soft luminous halo
    return group;
  };

  // Load and sample the 8 AI logo point clouds from pre-sampled binary coordinate data.
  // This achieves the exact 3D hologram look of the GLB meshes in milliseconds with 0 loading freeze!
  const loadLogoPoints = async (name) => {
    try {
      const resp = await fetch(`/models/${name}.bin`);
      if (!resp.ok) throw new Error(`Model not pre-sampled: ${name}`);
      const buf = await resp.arrayBuffer();
      const fullArray = new Float32Array(buf);

      // Dense sampling for the hologram halo layer (identity now comes from the PNG icon,
      // so these points are pure atmosphere and can afford to be denser + dimmer)
      const count = 2000;
      const sampled = new Float32Array(count * 3);
      const stride = Math.floor((fullArray.length / 3) / count);
      // stride 0 would collapse all samples onto vertex 0 — treat as unusable bin
      if (stride < 1) throw new Error(`Bin too small for ${count} samples: ${name}`);
      for (let i = 0; i < count; i++) {
        const idx = i * stride;
        // Normalized coordinate is in [-0.5, 0.5] range; 0.24 makes the halo slightly
        // wider than the PNG icon plane (0.22) so it fringes out around the icon edges
        sampled[i * 3] = fullArray[idx * 3] * 0.24;
        sampled[i * 3 + 1] = fullArray[idx * 3 + 1] * 0.24;
        sampled[i * 3 + 2] = fullArray[idx * 3 + 2] * 0.24;
      }
      return sampled;
    } catch (e) {
      console.warn(e.message);
      return null;
    }
  };

  // Brand PNG icons (transparent 512x512 badge renders under public/logos/) —
  // this is the primary identity layer; the point cloud is only an energy halo behind it
  const texLoader = new THREE.TextureLoader();
  const loadLogoTexture = async (name) => {
    try {
      const tex = await texLoader.loadAsync(`/logos/${name}.png`);
      tex.colorSpace = THREE.SRGBColorSpace;
      tex.anisotropy = Math.min(8, renderer.capabilities.getMaxAnisotropy());
      return tex;
    } catch (e) {
      console.warn(`Logo texture missing: /logos/${name}.png`);
      return null;
    }
  };

  // Load all halo point clouds and icon textures in parallel
  const allLogos = ORBITS.flatMap((o) => o.logos);
  const [pointsList, texList] = await Promise.all([
    Promise.all(allLogos.map((n) => loadLogoPoints(BIN_NAMES[n]))),
    Promise.all(allLogos.map((n) => loadLogoTexture(n)))
  ]);
  const logoPoints = {};
  const logoTex = {};
  allLogos.forEach((n, i) => {
    logoPoints[n] = pointsList[i];
    logoTex[n] = texList[i];
  });

  // Build both orbits and place their satellites, evenly spaced along each ring
  ORBITS.forEach((cfg) => {
    const orbitGroup = createOrbit(cfg);
    const step = (Math.PI * 2) / cfg.logos.length;
    cfg.logos.forEach((name, i) => {
      createSatellite3D(
        logoPoints[name],
        logoTex[name],
        brandColors[name],
        name,
        orbitGroup,
        cfg.radius,
        cfg.phase + i * step,
        cfg.speed
      );
    });
  });
}

// 3D satellite token, three layers (back to front):
//   1. hologram halo   — brand-colored point cloud, pure atmosphere, sits behind the icon
//   2. weak glass base — dim disc + thin rim ring, must never outshine the icon
//   3. brand PNG icon  — the identity layer, full original colors, always camera-facing
function createSatellite3D(sampledVertices, logoTexture, brandColor, modelName, parentOrbit, radius, initialAngle, speed) {
  const satelliteGroup = new THREE.Group();

  // 1. Hologram halo point cloud (behind the icon, fringes out around its edges)
  let logoPoints = null;
  if (sampledVertices) {
    const geom = new THREE.BufferGeometry();
    geom.setAttribute('position', new THREE.BufferAttribute(sampledVertices, 3));

    const mat = new THREE.PointsMaterial({
      color: new THREE.Color(brandColor),
      size: 0.007,
      transparent: true,
      opacity: 0.45,
      blending: THREE.AdditiveBlending,
      depthWrite: false
    });
    logoPoints = new THREE.Points(geom, mat);
    logoPoints.position.z = -0.03;
    logoPoints.raycast = () => {}; // atmosphere only — never a hover/click target
    satelliteGroup.add(logoPoints);
  }

  // 1b. Soft brand-color glow backing — separates dark badges from the deep-space
  // background and reads as satellite energy. Very dim; boosted slightly on hover.
  const glowMat = new THREE.SpriteMaterial({
    map: createGlowSpriteTexture(brandColor),
    transparent: true,
    opacity: 0.16,
    blending: THREE.AdditiveBlending,
    depthWrite: false
  });
  const glowBacking = new THREE.Sprite(glowMat);
  glowBacking.scale.set(0.55, 0.55, 1.0);
  glowBacking.position.z = -0.06;
  glowBacking.raycast = () => {}; // atmosphere only — never a hover/click target
  satelliteGroup.add(glowBacking);

  // 2a. Thin rim ring (weakened: atmosphere accent, not a UI button border)
  const rimGeom = new THREE.TorusGeometry(0.128, 0.006, 8, 32);
  const rimMat = new THREE.MeshStandardMaterial({
    color: new THREE.Color(brandColor),
    metalness: 0.9,
    roughness: 0.1,
    transparent: true,
    opacity: 0.4
  });
  const rimMesh = new THREE.Mesh(rimGeom, rimMat);
  satelliteGroup.add(rimMesh);

  // 2b. Faint glass disc base
  const discGeom = new THREE.CylinderGeometry(0.128, 0.128, 0.012, 32);
  const discMat = new THREE.MeshPhongMaterial({
    color: 0x90d0ff,
    transparent: true,
    opacity: 0.06,
    shininess: 120,
    specular: 0xffffff,
    side: THREE.DoubleSide,
    depthWrite: false
  });
  const discMesh = new THREE.Mesh(discGeom, discMat);
  discMesh.rotation.x = Math.PI / 2;
  satelliteGroup.add(discMesh);

  // 3. Brand PNG icon plane — the readable layer. Under EffectComposer, per-material
  // toneMapped is ignored (OutputPass ACES-tone-maps the whole frame), so original
  // brand colors are preserved by baking inverse-ACES into the textures instead —
  // see the display-compensation step in scripts/process_logos.cjs.
  let logoMesh = null;
  if (logoTexture) {
    // 0.22 world units ≈ 8% of viewport height at the orbit's near point, fading to
    // ~4% on the far side via distFactor — readable at 1440px, far below the bulb's weight
    const logoGeom = new THREE.PlaneGeometry(0.22, 0.22);
    const logoMat = new THREE.MeshBasicMaterial({
      map: logoTexture,
      transparent: true,
      toneMapped: false,
      depthWrite: false
    });
    logoMesh = new THREE.Mesh(logoGeom, logoMat);
    logoMesh.position.z = 0.02;
    satelliteGroup.add(logoMesh);
  }

  satelliteGroup.userData = {
    modelName,
    radius,
    angle: initialAngle,
    speed,
    hoverScale: 1.0,
    clickPulse: 1.0,
    rimMaterial: rimMat,
    glowMaterial: glowMat,
    pointsMaterial: logoPoints ? logoPoints.material : null,
    logoMaterial: logoMesh ? logoMesh.material : null
  };

  parentOrbit.add(satelliteGroup);
  satellites.push(satelliteGroup);
}

// ==========================================
// 5. Interaction Setup
// ==========================================

const raycaster = new THREE.Raycaster();
// Default Points threshold is 1 WORLD unit — with the 2000-point halos that would
// make every satellite's hit zone ~10x its visible badge. Keep it near icon scale.
raycaster.params.Points.threshold = 0.02;
const mouse = new THREE.Vector2();
// No hover checks until the pointer has actually entered the page — mouse defaults
// to NDC (0,0), which is the center of the screen, not "no cursor"
let hasPointer = false;

function setupInteractions() {
  window.addEventListener('mousemove', (event) => {
    mouse.x = (event.clientX / window.innerWidth) * 2 - 1;
    mouse.y = -(event.clientY / window.innerHeight) * 2 + 1;
    hasPointer = true;
  });

  // Cursor left the page: stop per-frame hover checks against a stale position
  document.addEventListener('mouseleave', () => {
    hasPointer = false;
    clearHover();
  });

  window.addEventListener('click', (event) => {
    if (event.target.id !== 'webgl-canvas') return;

    raycaster.setFromCamera(mouse, camera);
    const intersects = raycaster.intersectObjects(satellites, true);

    if (intersects.length > 0) {
      let sat = intersects[0].object;
      while (sat && !satellites.includes(sat)) {
        sat = sat.parent;
      }

      if (sat) {
        const model = sat.userData.modelName;

        // Tween the proxy factor, not sat.scale — animate() rewrites sat.scale every frame
        gsap.to(sat.userData, {
          clickPulse: 1.3,
          duration: 0.15,
          yoyo: true,
          repeat: 1,
          overwrite: 'auto'
        });

        triggerCoreSurge(model);
      }
    }
  });

  window.addEventListener('resize', onWindowResize);
}

function checkSatelliteHover() {
  raycaster.setFromCamera(mouse, camera);
  const intersects = raycaster.intersectObjects(satellites, true);

  if (intersects.length > 0) {
    let sat = intersects[0].object;
    while (sat && !satellites.includes(sat)) {
      sat = sat.parent;
    }

    if (sat && hoveredGroup !== sat) {
      if (hoveredGroup) {
        gsap.to(hoveredGroup.userData, { hoverScale: 1.0, duration: 0.3, overwrite: 'auto' });
      }
      hoveredGroup = sat;
      document.body.style.cursor = 'pointer';

      gsap.to(sat.userData, { hoverScale: 1.2, duration: 0.3, ease: 'power2.out', overwrite: 'auto' });
      updateHUD(sat.userData.modelName);
    }
  } else {
    clearHover();
  }
}

function clearHover() {
  if (!hoveredGroup) return;
  gsap.to(hoveredGroup.userData, { hoverScale: 1.0, duration: 0.3, overwrite: 'auto' });
  hoveredGroup = null;
  document.body.style.cursor = 'default';
  resetHUD();
}

function triggerCoreSurge(modelName) {
  const brandColor = brandColors[modelName] || '#00f0ff';

  // Light color surge
  gsap.to(pointLight.color, {
    r: new THREE.Color(brandColor).r,
    g: new THREE.Color(brandColor).g,
    b: new THREE.Color(brandColor).b,
    duration: 0.4
  });

  // Surge Filament LED Cap scale
  coreMesh.material.color.set(brandColor);
  gsap.fromTo(coreMesh.scale,
    { x: 1.0, y: 1.0, z: 1.0 },
    { x: 1.8, y: 1.8, z: 1.8, duration: 0.35, yoyo: true, repeat: 1, ease: 'power2.out' }
  );

  // Soft surge of diffuse volumetric glow sprite
  gsap.to(glowSprite.material, { opacity: 0.55, duration: 0.35, yoyo: true, repeat: 1 });

  document.documentElement.style.setProperty('--glow-cyan', brandColor);
}

let hudTimeout;
function updateHUD(modelName) {
  if (hudTimeout) clearTimeout(hudTimeout);

  const details = modelDetails[modelName] || modelDetails.dengpao;

  gsap.to(uiElements.hudPanel, {
    opacity: 0,
    y: 8,
    duration: 0.15,
    onComplete: () => {
      uiElements.hudName.textContent = details.name;
      uiElements.hudProvider.textContent = details.provider;
      uiElements.hudDesc.textContent = details.desc;
      uiElements.hudTelemetry.textContent = details.telemetry;

      uiElements.hudPanel.style.borderLeftColor = brandColors[modelName] || '#00f0ff';

      gsap.to(uiElements.hudPanel, {
        opacity: 1,
        y: 0,
        duration: 0.35,
        ease: 'power2.out'
      });
    }
  });
}

function resetHUD() {
  hudTimeout = setTimeout(() => {
    updateHUD('dengpao');

    gsap.to(pointLight.color, {
      r: new THREE.Color('#00f0ff').r,
      g: new THREE.Color('#00f0ff').g,
      b: new THREE.Color('#00f0ff').b,
      duration: 0.8
    });

    document.documentElement.style.setProperty('--glow-cyan', '#00f0ff');
  }, 1000);
}

function onWindowResize() {
  camera.aspect = window.innerWidth / window.innerHeight;
  camera.updateProjectionMatrix();
  renderer.setSize(window.innerWidth, window.innerHeight);
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  composer.setSize(window.innerWidth, window.innerHeight);
}

// ==========================================
// 6. Animation Loop
// ==========================================

const clock = new THREE.Clock();

// Scratch objects for per-frame satellite billboard math (avoid per-frame allocation)
const _parentQuat = new THREE.Quaternion();
const _worldPos = new THREE.Vector3();
const _bulbView = new THREE.Vector3();
const _cursorRayDir = new THREE.Vector3(0, 0, -1);
let lastElapsedTime = 0;

function animate() {
  requestAnimationFrame(animate);

  const elapsedTime = clock.getElapsedTime();

  // Advance camera damping BEFORE the satellite loop so billboards face this
  // frame's camera pose, not last frame's
  controls.update();
  camera.updateMatrixWorld();

  // Feed the orbit-line shaders: energy-flow time, bulb center in view space (drives
  // the far-side mask) and current satellite angles (drives the comet wakes) — the
  // angle formula matches the satellite loop below exactly, so wakes track the badges
  _bulbView.set(0, 0, 0).applyMatrix4(camera.matrixWorldInverse);
  orbitMaterials.forEach(({ material, cfg }) => {
    const u = material.uniforms;
    u.uTime.value = elapsedTime;
    u.uBulbView.value.copy(_bulbView);
    const step = (Math.PI * 2) / cfg.logos.length;
    for (let i = 0; i < cfg.logos.length; i++) {
      u.uSatAngles.value[i] = (cfg.phase + i * step + cfg.speed * elapsedTime) % (Math.PI * 2);
    }
  });

  if (particleMaterial) {
    const u = particleMaterial.uniforms;
    u.uTime.value = elapsedTime;

    // Fluid mouse repulsion field: the field's ray direction eases toward the live
    // cursor (elastic lag = fluid feel) and its strength fades in fast / out slow,
    // so particles disperse on approach and gently flow home when the cursor leaves.
    const dt = Math.min(Math.max(elapsedTime - lastElapsedTime, 0.001), 0.05);
    if (hasPointer) {
      _cursorRayDir.set(mouse.x, mouse.y, 0.5).unproject(camera).sub(camera.position).normalize();
    }
    u.uMouseOrigin.value.copy(camera.position);
    u.uMouseDir.value.lerp(_cursorRayDir, 1 - Math.exp(-9 * dt)).normalize();
    const strengthTarget = hasPointer ? 1 : 0;
    const ease = strengthTarget > u.uRepelStrength.value ? 5 : 2.2; // engage fast, release gently
    u.uRepelStrength.value += (strengthTarget - u.uRepelStrength.value) * (1 - Math.exp(-ease * dt));
  }
  lastElapsedTime = elapsedTime;

  // Gentle slow core spin
  if (particleSystem) {
    particleSystem.rotation.y = elapsedTime * 0.02;
  }
  if (bulbGroup) {
    bulbGroup.rotation.y = elapsedTime * 0.02;
  }

  // Update satellites
  satellites.forEach(sat => {
    const currentAngle = sat.userData.angle + sat.userData.speed * elapsedTime;
    const r = sat.userData.radius;

    // Local positions in parent tilted orbit group space
    sat.position.x = r * Math.cos(currentAngle);
    sat.position.z = r * Math.sin(currentAngle);
    sat.position.y = Math.sin(elapsedTime * 1.5 + sat.userData.angle) * 0.03;

    // True camera-facing billboard: the parent orbit group is tilted, so we must
    // cancel its world rotation first (local = parentQuat⁻¹ * cameraQuat)
    sat.parent.getWorldQuaternion(_parentQuat);
    sat.quaternion.copy(_parentQuat.invert()).multiply(camera.quaternion);

    // Calculate view-space distance to camera to apply spatial depth fading & scaling
    sat.getWorldPosition(_worldPos);
    const dist = camera.position.distanceTo(_worldPos);

    // Closest distance is ~3.2, Furthest is ~5.8
    // Map factor [1.0 (close) to 0.5 (far)] — floor raised so far-side badges stay identifiable
    const distFactor = THREE.MathUtils.clamp(
      THREE.MathUtils.mapLinear(dist, 3.2, 5.8, 1.0, 0.5),
      0.5,
      1.0
    );

    // hoverScale 1.0 → 1.2 maps to a 0 → +0.25 opacity boost on the halo particles
    const hoverBoost = (sat.userData.hoverScale - 1.0) * 1.25;

    // Halo particles: micro twinkle only, dim enough to never bury the PNG icon.
    // 0.15 + 0.40 * distFactor spans exactly the 0.35 (far) – 0.55 (near) opacity band
    if (sat.userData.pointsMaterial) {
      sat.userData.pointsMaterial.size = 0.006 + 0.002 * Math.sin(elapsedTime * 2.5 + sat.userData.angle);
      sat.userData.pointsMaterial.opacity = Math.min(0.7, 0.15 + 0.40 * distFactor + hoverBoost);
    }

    // Brand icon stays readable even on the far side of the orbit
    if (sat.userData.logoMaterial) {
      sat.userData.logoMaterial.opacity = 0.35 + 0.65 * distFactor;
    }

    // Glow backing: dim brand aura, brightens on hover (halo enhancement)
    sat.userData.glowMaterial.opacity = (0.16 + (sat.userData.hoverScale - 1.0) * 0.9) * distFactor;

    // Apply combined scale (hover scale * click pulse * depth factor)
    const combinedScale = sat.userData.hoverScale * sat.userData.clickPulse * distFactor;
    sat.scale.set(combinedScale, combinedScale, combinedScale);

    // Fade metallic rim material dynamically (weakened: accent, not highlight)
    sat.userData.rimMaterial.opacity = 0.4 * distFactor;
  });

  composer.render();

  // Satellites orbit continuously, so hover must be re-tested even while the mouse
  // is still — run after render so the raycast sees this frame's world matrices
  if (hasPointer) checkSatelliteHover();
}

init().catch(err => {
  console.error('Fatal initialization error:', err);
});
