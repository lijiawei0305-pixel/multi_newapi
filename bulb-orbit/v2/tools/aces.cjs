// Inverse ACES filmic (matches three.js r160 tonemapping_pars_fragment exactly)
// ==========================================
// Ported verbatim from yun's scripts/process_logos.cjs (lines 49-119). Do not alter
// the constants below — they must stay bit-identical to three.js's tone mapping so
// that ACES(invACES(c)) == c holds.
//
// three.js forward transform (exposure 1.0):
//   out = saturate( ACESOutputMat * fit( ACESInputMat * (c / 0.6) ) )
//   fit(v) = (v^2 + 0.0245786 v - 0.000090537) / (0.983729 v^2 + 0.432951 v + 0.238081)
// Matrices below are row-major (the GLSL mat3(...) constructor lists COLUMNS).
'use strict'

const ACES_INPUT = [
  [0.59719, 0.35458, 0.04823],
  [0.07600, 0.90834, 0.01566],
  [0.02840, 0.13383, 0.83777],
];
const ACES_OUTPUT = [
  [1.60475, -0.53108, -0.07367],
  [-0.10208, 1.10813, -0.00605],
  [-0.00327, -0.07276, 1.07602],
];

function invertMat3(m) {
  const [[a, b, c], [d, e, f], [g, h, i]] = m;
  const A = e * i - f * h, B = c * h - b * i, C = b * f - c * e;
  const det = a * A + d * B + g * C;
  return [
    [A / det, B / det, C / det],
    [(f * g - d * i) / det, (a * i - c * g) / det, (c * d - a * f) / det],
    [(d * h - e * g) / det, (b * g - a * h) / det, (a * e - b * d) / det],
  ];
}

const ACES_INPUT_INV = invertMat3(ACES_INPUT);
const ACES_OUTPUT_INV = invertMat3(ACES_OUTPUT);

const mulMat3 = (m, v) => [
  m[0][0] * v[0] + m[0][1] * v[1] + m[0][2] * v[2],
  m[1][0] * v[0] + m[1][1] * v[1] + m[1][2] * v[2],
  m[2][0] * v[0] + m[2][1] * v[1] + m[2][2] * v[2],
];

// Inverse of fit(): solve the quadratic fit(v) = f for v >= 0 (positive root).
// fit() has an asymptote at 1/0.983729 ≈ 1.0165, so clamp f just below 1 — display
// values that close to white are unreachable from LDR texture inputs anyway.
function fitInv(f) {
  f = Math.min(f, 0.999);
  const A = 1 - 0.983729 * f;
  const B = 0.0245786 - 0.432951 * f;
  const C = -(0.000090537 + 0.238081 * f);
  return (-B + Math.sqrt(Math.max(B * B - 4 * A * C, 0))) / (2 * A);
}

function forwardAcesFilmic(rgb) {
  const v = mulMat3(ACES_INPUT, rgb.map((x) => x / 0.6));
  const f = v.map((x) => (x * (x + 0.0245786) - 0.000090537) / (x * (0.983729 * x + 0.432951) + 0.238081));
  return mulMat3(ACES_OUTPUT, f).map((x) => Math.min(Math.max(x, 0), 1));
}

// Linear display color -> linear texture color whose ACES output is that display color.
// Out-of-gamut / near-white targets clamp to the closest LDR-representable value.
function inverseAcesFilmic(rgb) {
  const f = mulMat3(ACES_OUTPUT_INV, rgb).map((x) => Math.max(x, 0));
  const v = f.map(fitInv);
  return mulMat3(ACES_INPUT_INV, v).map((x) => Math.min(Math.max(x * 0.6, 0), 1));
}

const srgbToLinear = (u) => {
  const c = u / 255;
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
};
const linearToSrgb = (l) => {
  const c = l <= 0.0031308 ? l * 12.92 : 1.055 * Math.pow(l, 1 / 2.4) - 0.055;
  return Math.round(Math.min(Math.max(c, 0), 1) * 255);
};

module.exports = { forwardAcesFilmic, inverseAcesFilmic, srgbToLinear, linearToSrgb };
