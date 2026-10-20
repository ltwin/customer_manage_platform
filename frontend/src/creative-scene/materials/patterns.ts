import type { MaterialEntry } from './catalog.ts'

// The procedural presets' shading (design「材质着色」): each preset is a GLSL
// function patternEval() giving base colour, roughness, metalness, a height
// in metres for bump, and emission. It reads vPatPos, the object-space
// position in true metres (so a scaled object does not stretch its
// pattern), vPatNormal for the projection weights, and its parameters as
// uniforms pm_<name>.
//
// Noise presets are solid textures evaluated in 3D: no projection, no
// seams. Flat patterns (brick, tile, planks, fabric) are projected along the
// three axes and blended by pow(|n|, 4); their joints may not meet at
// corners, which is accepted.
//
// Anti-aliasing, since multisampling does not reach inside a shader:
//  - hard edges (joints, gaps) are smoothsteps as wide as a pixel (patAAStep);
//  - fbm octaves finer than about two pixels fade out (patFbm), and the
//    height uses the same faded values, so bumps flatten with distance;
//  - a joint narrower than a pixel blends towards its average share.
//
// A published preset never changes: a different look is a new @2.

/** Shared by every preset: the output, hashes, value noise, fbm, Voronoi, projection. */
export const commonGLSL = /* glsl */ `
varying vec3 vPatPos;
varying vec3 vPatNormal;

struct PatOut { vec3 albedo; float rough; float metal; float height; vec3 emission; };

PatOut patMix3(PatOut a, PatOut b, PatOut c, vec3 w) {
  PatOut o;
  o.albedo = a.albedo * w.x + b.albedo * w.y + c.albedo * w.z;
  o.rough = a.rough * w.x + b.rough * w.y + c.rough * w.z;
  o.metal = a.metal * w.x + b.metal * w.y + c.metal * w.z;
  o.height = a.height * w.x + b.height * w.y + c.height * w.z;
  o.emission = a.emission * w.x + b.emission * w.y + c.emission * w.z;
  return o;
}

PatOut patOut(vec3 albedo, float rough, float metal, float height) {
  PatOut o;
  o.albedo = albedo;
  o.rough = clamp(rough, 0.0, 1.0);
  o.metal = clamp(metal, 0.0, 1.0);
  o.height = height;
  o.emission = vec3(0.0);
  return o;
}

vec3 patTriWeights(vec3 n) {
  vec3 a = pow(abs(normalize(n)), vec3(4.0));
  return a / (a.x + a.y + a.z);
}

// Hashes without sin, so every GPU gives the same pattern.
float patHash13(vec3 p3) {
  p3 = fract(p3 * 0.1031);
  p3 += dot(p3, p3.zyx + 31.32);
  return fract((p3.x + p3.y) * p3.z);
}

float patHash12(vec2 p) {
  vec3 p3 = fract(vec3(p.xyx) * 0.1031);
  p3 += dot(p3, p3.yzx + 33.33);
  return fract((p3.x + p3.y) * p3.z);
}

float patNoise(vec3 p) {
  vec3 i = floor(p);
  vec3 f = fract(p);
  vec3 u = f * f * (3.0 - 2.0 * f);
  return mix(
    mix(mix(patHash13(i), patHash13(i + vec3(1, 0, 0)), u.x), mix(patHash13(i + vec3(0, 1, 0)), patHash13(i + vec3(1, 1, 0)), u.x), u.y),
    mix(mix(patHash13(i + vec3(0, 0, 1)), patHash13(i + vec3(1, 0, 1)), u.x), mix(patHash13(i + vec3(0, 1, 1)), patHash13(i + vec3(1, 1, 1)), u.x), u.y),
    u.z);
}

// Metres one pixel covers here.
float patFootprint() {
  return max(length(fwidth(vPatPos)), 1e-6);
}

// fbm around 0.5 starting at freq cycles per metre. An octave whose
// wavelength is under about two pixels (fp metres each) fades out.
float patFbm(vec3 p, float freq, float fp, int octaves) {
  float sum = 0.0;
  float norm = 0.0;
  float amp = 0.5;
  float f = freq;
  for (int i = 0; i < 7; i++) {
    if (i >= octaves) break;
    float fade = 1.0 - smoothstep(0.25, 0.5, f * fp);
    sum += amp * fade * (patNoise(p * f + float(i) * 17.31) - 0.5);
    norm += amp;
    amp *= 0.5;
    f *= 2.0;
  }
  return 0.5 + sum / norm;
}

// Distances to the nearest and second nearest cell point.
vec2 patVoronoi(vec3 p) {
  vec3 i = floor(p);
  vec3 f = fract(p);
  float d1 = 8.0;
  float d2 = 8.0;
  for (int z = -1; z <= 1; z++)
    for (int y = -1; y <= 1; y++)
      for (int x = -1; x <= 1; x++) {
        vec3 g = vec3(float(x), float(y), float(z));
        vec3 o = vec3(patHash13(i + g), patHash13(i + g + 11.1), patHash13(i + g + 23.7));
        vec3 r = g + o - f;
        float d = dot(r, r);
        if (d < d1) { d2 = d1; d1 = d; } else if (d < d2) { d2 = d; }
      }
  return vec2(sqrt(d1), sqrt(d2));
}

// 0 below edge, 1 above, over the width of a pixel.
float patAAStep(float edge, float x) {
  float w = max(fwidth(x), 1e-6);
  return smoothstep(edge - w, edge + w, x);
}

// A joint of width jw metres at distance d from the cell edge: 1 in the
// joint. Narrower than a pixel, it tends to its share of the cell (share).
float patJoint(float d, float jw, float share) {
  float j = 1.0 - patAAStep(jw * 0.5, d);
  return mix(j, share, smoothstep(0.5, 1.5, fwidth(d) / jw));
}

// The normal bent by the height's slope along the surface (surface
// gradient), resolution independent; P and N in view space.
vec3 patBumpNormal(vec3 P, vec3 N, float h) {
  vec3 dpdx = dFdx(P);
  vec3 dpdy = dFdy(P);
  vec3 r1 = cross(dpdy, N);
  vec3 r2 = cross(N, dpdx);
  float det = dot(dpdx, r1);
  if (abs(det) < 1e-20) return N;
  vec3 grad = (dFdx(h) * r1 + dFdy(h) * r2) / det;
  return normalize(N - grad);
}
`

/** A flat pattern f(uv, fp) projected along the three axes. */
const planar = (fn: string) => /* glsl */ `
PatOut patternEval() {
  float fp = patFootprint();
  vec3 w = patTriWeights(vPatNormal);
  return patMix3(${fn}(vPatPos.zy, fp), ${fn}(vPatPos.xz, fp), ${fn}(vPatPos.xy, fp), w);
}
`

/** patternEval() of each preset. */
export const patternGLSL: Record<string, string> = {
  'paint@1': /* glsl */ `
PatOut patternEval() {
  float fp = patFootprint();
  float n = patFbm(vPatPos, 3.0, fp, 4);
  return patOut(pm_color * (0.97 + 0.06 * n), pm_roughness, pm_metalness, 0.0002 * patFbm(vPatPos, 60.0, fp, 3));
}
`,
  'plaster@1': /* glsl */ `
PatOut patternEval() {
  float fp = patFootprint();
  float n = patFbm(vPatPos, 1.5, fp, 5);
  float fine = patFbm(vPatPos, 12.0, fp, 4);
  vec3 albedo = pm_color * mix(1.0, 0.72 + 0.56 * n, pm_mottle);
  return patOut(albedo, pm_roughness + 0.08 * (fine - 0.5), pm_metalness, 0.0015 * fine + 0.002 * n);
}
`,
  'concrete@1': /* glsl */ `
PatOut patternEval() {
  float fp = patFootprint();
  float n = patFbm(vPatPos, 2.0, fp, 5);
  float fine = patFbm(vPatPos, 25.0, fp, 3);
  float pores = (1.0 - smoothstep(0.12, 0.18, patNoise(vPatPos * 60.0))) * (1.0 - smoothstep(0.25, 0.5, 60.0 * fp));
  float stain = smoothstep(0.55, 0.8, patFbm(vPatPos + 7.3, 0.35, fp, 4)) * pm_stains;
  vec3 albedo = pm_color * (0.85 + 0.3 * n) * (1.0 - 0.45 * stain) * (1.0 - 0.3 * pores);
  return patOut(albedo, pm_roughness, pm_metalness, 0.0015 * fine - 0.0015 * pores);
}
`,
  'stone_brick@1': /* glsl */ `
PatOut patBrick(vec2 uv, float fp) {
  float h = pm_brick_size;
  float w = 2.0 * h;
  float jw = clamp(0.06 * h, 0.004, 0.02);
  vec2 c = vec2(uv.x / w, uv.y / h);
  c.x += 0.5 * mod(floor(c.y), 2.0);
  vec2 id = floor(c);
  vec2 f = fract(c);
  float d = min(min(f.x, 1.0 - f.x) * w, min(f.y, 1.0 - f.y) * h);
  float joint = patJoint(d, jw, jw / h + jw / w);
  float tint = patHash12(id);
  float n = patFbm(vec3(uv, tint * 13.0), 6.0, fp, 4);
  vec3 brick = pm_color * (0.82 + 0.3 * tint) * (0.9 + 0.2 * n);
  float bevel = smoothstep(0.0, 0.015, d - jw * 0.5);
  return patOut(mix(brick, pm_mortar_color, joint), mix(pm_roughness, pm_roughness + 0.1, joint), pm_metalness, -0.01 * (1.0 - bevel) + 0.002 * n);
}
${planar('patBrick')}`,
  'stone@1': /* glsl */ `
PatOut patternEval() {
  float s = pm_scale;
  vec3 q = vPatPos / s;
  float fp = patFootprint() / s;
  float n = patFbm(q, 1.0, fp, 6);
  vec2 v = patVoronoi(q * 2.0 + 0.5 * (n - 0.5));
  float gap = v.y - v.x;
  float cw = max(fwidth(gap), 1e-4);
  float crack = (1.0 - smoothstep(0.02, 0.02 + cw, gap)) * (1.0 - smoothstep(0.5, 1.0, cw / 0.05));
  vec3 albedo = pm_color * (0.7 + 0.6 * n) * (1.0 - 0.5 * crack);
  return patOut(albedo, pm_roughness, pm_metalness, s * (0.03 * n - 0.01 * crack));
}
`,
  'marble@1': /* glsl */ `
PatOut patternEval() {
  float s = pm_scale;
  vec3 q = vPatPos / s;
  float fp = patFootprint() / s;
  float turb = patFbm(q, 1.5, fp, 6);
  float t = q.x * 1.3 + q.y * 0.4 + 4.0 * turb;
  float vein = pow(1.0 - abs(sin(t * 3.14159265)), 6.0);
  vein = mix(vein, 0.15, smoothstep(0.3, 0.8, fwidth(t)));
  vec3 albedo = mix(pm_color * (0.95 + 0.1 * turb), pm_vein_color, vein * 0.85);
  return patOut(albedo, pm_roughness + 0.05 * vein, pm_metalness, -0.0005 * vein);
}
`,
  'tile@1': /* glsl */ `
PatOut patTile(vec2 uv, float fp) {
  float s = pm_tile_size;
  float jw = 0.004;
  vec2 c = uv / s;
  vec2 id = floor(c);
  vec2 f = fract(c);
  float d = min(min(f.x, 1.0 - f.x), min(f.y, 1.0 - f.y)) * s;
  float joint = patJoint(d, jw, 2.0 * jw / s);
  float tint = patHash12(id);
  float bevel = smoothstep(0.0, 0.004, d - jw * 0.5);
  return patOut(mix(pm_color * (0.96 + 0.08 * tint), pm_grout_color, joint), mix(pm_roughness, 0.85, joint), pm_metalness, -0.003 * (1.0 - bevel));
}
${planar('patTile')}`,
  'wood@1': /* glsl */ `
PatOut patternEval() {
  float s = pm_scale;
  vec3 q = vPatPos / s;
  float fp = patFootprint() / s;
  float warp = patFbm(q * vec3(1.0, 0.15, 1.0), 2.0, fp, 4);
  float r = length(q.xz + 0.3) * 8.0 + warp * 3.0;
  float ring = smoothstep(0.0, 0.25, fract(r)) * (1.0 - smoothstep(0.6, 1.0, fract(r)));
  ring = mix(ring, 0.55, smoothstep(0.3, 0.8, fwidth(r)));
  float grain = patFbm(q * vec3(8.0, 0.5, 8.0), 4.0, fp * 8.0, 4);
  vec3 albedo = pm_color * mix(0.72, 1.12, ring) * (0.92 + 0.16 * grain);
  return patOut(albedo, pm_roughness, pm_metalness, 0.0004 * ring + 0.0003 * grain);
}
`,
  'wood_plank@1': /* glsl */ `
PatOut patPlank(vec2 uv, float fp) {
  float wv = pm_plank_width;
  float len = wv * 8.0;
  float row = floor(uv.y / wv);
  float x = uv.x / len + patHash12(vec2(row, 3.7));
  vec2 id = vec2(floor(x), row);
  vec2 f = vec2(fract(x), fract(uv.y / wv));
  float d = min(min(f.x, 1.0 - f.x) * len, min(f.y, 1.0 - f.y) * wv);
  float gap = patJoint(d, 0.003, 0.003 / wv + 0.003 / len);
  float tint = patHash12(id + 1.3);
  float grain = patFbm(vec3(uv.x * 0.6, f.y * wv * 25.0, tint * 9.0), 4.0, fp * 25.0, 5);
  vec3 plank = pm_color * (0.85 + 0.3 * tint) * (0.85 + 0.3 * grain);
  return patOut(mix(plank, pm_color * 0.25, gap), pm_roughness + 0.1 * gap, pm_metalness, -0.002 * gap + 0.0003 * grain);
}
${planar('patPlank')}`,
  'metal@1': /* glsl */ `
PatOut patternEval() {
  float fp = patFootprint();
  float n = patFbm(vPatPos, 3.0, fp, 5);
  float r = patFbm(vPatPos + 3.1, 1.5, fp, 6);
  float mask = smoothstep(1.0 - pm_rust, 1.15 - pm_rust, r);
  vec3 rust = vec3(0.32, 0.13, 0.05) * (0.7 + 0.6 * n);
  vec3 albedo = mix(pm_color * (0.9 + 0.2 * n), rust, mask);
  float rough = mix(pm_roughness + 0.1 * (n - 0.5), 0.92, mask);
  return patOut(albedo, rough, mix(pm_metalness, 0.0, mask), 0.0008 * mask * n + 0.0002 * patFbm(vPatPos, 40.0, fp, 3));
}
`,
  'fabric@1': /* glsl */ `
PatOut patWeave(vec2 uv, float fp) {
  float s = pm_scale;
  vec2 c = uv / s;
  vec2 f = fract(c);
  float over = mod(floor(c.x) + floor(c.y), 2.0);
  float h = over > 0.5 ? sin(3.14159265 * f.x) : sin(3.14159265 * f.y);
  vec2 fw = fwidth(c);
  float fade = 1.0 - smoothstep(0.25, 0.5, max(fw.x, fw.y));
  h = mix(0.6, h, fade);
  float n = patFbm(vec3(uv, 0.0), 8.0, fp, 4);
  return patOut(pm_color * (0.8 + 0.25 * h) * (0.95 + 0.1 * n), pm_roughness, pm_metalness, 0.3 * s * h * fade);
}
${planar('patWeave')}`,
  'dirt@1': /* glsl */ `
PatOut patternEval() {
  float fp = patFootprint();
  float n = patFbm(vPatPos, 1.0, fp, 6);
  float fine = patFbm(vPatPos, 15.0, fp, 4);
  vec2 v = patVoronoi(vPatPos * 25.0);
  float pebble = (1.0 - smoothstep(0.15, 0.25, v.x)) * (1.0 - smoothstep(0.25, 0.5, 25.0 * fp));
  vec3 albedo = pm_color * (0.75 + 0.5 * n) * (0.9 + 0.2 * fine) * (1.0 + 0.25 * pebble);
  return patOut(albedo, pm_roughness, pm_metalness, 0.01 * n + 0.002 * fine + 0.003 * pebble);
}
`,
  'grass@1': /* glsl */ `
PatOut patternEval() {
  float fp = patFootprint();
  float n = patFbm(vPatPos, 0.8, fp, 5);
  float blades = patFbm(vPatPos, 60.0, fp, 3);
  float dry = smoothstep(0.55, 0.8, patFbm(vPatPos + 5.0, 0.3, fp, 4));
  vec3 tint = mix(pm_color, pm_color * vec3(1.35, 1.15, 0.6), dry);
  return patOut(tint * (0.7 + 0.5 * n) * (0.8 + 0.4 * blades), pm_roughness, pm_metalness, 0.004 * blades + 0.01 * n);
}
`,
  'emissive@1': /* glsl */ `
PatOut patternEval() {
  PatOut o = patOut(pm_color, pm_roughness, pm_metalness, 0.0);
  o.emission = pm_color * pm_strength;
  return o;
}
`,
}

/** The uniform declarations of a preset's parameters. */
export function uniformDeclarations(entry: MaterialEntry): string {
  return Object.entries(entry.params)
    .map(([k, p]) => `uniform ${p.type === 'color' ? 'vec3' : 'float'} pm_${k};`)
    .join('\n')
}
