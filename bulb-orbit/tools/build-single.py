#!/usr/bin/env python3
"""bulb-orbit 单文件拼装：把 three r160(+后期) 与 dengpao 点云 base64 注入 index.html 标记区。
用法：python3 tools/build-single.py   （在 bulb-orbit/ 下运行；幂等，可重复执行）
只在换 three 版本或换点云数据时需要重跑；日常改页面不用。仅 python3 标准库。"""
import base64, pathlib, re, sys

ROOT = pathlib.Path(__file__).resolve().parent.parent          # bulb-orbit/
SRC  = ROOT / 'tools' / 'vendor-src'
HTML = ROOT / 'index.html'
BIN  = ROOT / 'assets' / 'dengpao_points.bin'

# 依赖序：先 shader 对象，再 Pass 基类，再各通道（后者引用前者的顶层名）
ADDONS = ['CopyShader.js', 'LuminosityHighPassShader.js', 'OutputShader.js', 'Pass.js',
          'MaskPass.js', 'ShaderPass.js', 'RenderPass.js', 'EffectComposer.js',
          'UnrealBloomPass.js', 'OutputPass.js']

def three_to_consts(src: str) -> str:
    """three.module.min.js 内部名是压缩名，公开名只存在于末尾 export{A as B,...}。
    把它改写成 const B=A,... 使后续拼接的模块能用公开名。"""
    src = re.sub(r'//# sourceMappingURL=\S+\s*$', '', src)
    last = None
    for last in re.finditer(r'export\{([^}]*)\};?', src):
        pass
    if last is None:
        sys.exit('three.module.min.js: 未找到 export{...}，构建终止')
    body = src[:last.start()] + src[last.end():]
    pairs = []
    for item in last.group(1).split(','):
        item = item.strip()
        if not item:
            continue
        if ' as ' in item:
            a, b = [x.strip() for x in item.split(' as ')]
            if a != b:
                pairs.append(f'{b}={a}')
        # 无别名（a 即公开名）：顶层已存在同名声明，无需重绑
    return body + '\nconst ' + ','.join(pairs) + ';\n'

def strip_module(src: str) -> str:
    """examples/jsm 源码：去 import（依赖名已在拼接作用域内）、去 export 关键字。"""
    src = re.sub(r'import\s[\s\S]*?from\s*[\'"][^\'"]+[\'"];?', '', src)
    src = re.sub(r'export\s*\{[^}]*\};?', '', src)
    return (src.replace('export class ', 'class ')
               .replace('export const ', 'const ')
               .replace('export function ', 'function '))

def replace_block(html: str, tag: str, content: str) -> str:
    begin = f'/* ===== {tag}-BEGIN'
    end   = f'/* ===== {tag}-END'
    i = html.index(begin); i = html.index('*/', i) + 2
    j = html.index(end)
    return html[:i] + '\n' + content + '\n' + html[j:]

vendor = [three_to_consts((SRC / 'three.module.min.js').read_text())]
for name in ADDONS:
    vendor.append(f'\n/* ---- {name} ---- */\n' + strip_module((SRC / name).read_text()))
vendor.append("\nwindow.__bulb3dVendorOK = (typeof Scene==='function' && typeof EffectComposer==='function'"
              " && typeof UnrealBloomPass==='function' && typeof OutputPass==='function'"
              " && typeof ShaderPass==='function' && typeof RenderPass==='function');\n")
vendor_js = ''.join(vendor)
if '</script' in vendor_js.lower():
    sys.exit('vendor 代码含 </script>，会截断 HTML——需先处理再注入')

b64 = base64.b64encode(BIN.read_bytes()).decode()
chunks = ',\n'.join(f'"{b64[i:i+4000]}"' for i in range(0, len(b64), 4000))
data_js = (f'const DENGPAO_B64_CHUNKS=[\n{chunks}\n];\n'
           f'window.__bulbDataB64Len={len(b64)};\n')

html = HTML.read_text()
html = replace_block(html, 'BULB3D-VENDOR', vendor_js)
html = replace_block(html, 'BULB3D-DATA', data_js)
HTML.write_text(html)
print(f'ok: vendor={len(vendor_js)//1024}KB data={len(data_js)//1024}KB index.html={len(html)//1024}KB')
