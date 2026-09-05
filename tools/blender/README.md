# 母舰建模脚本

`axiom_ship.py` 用 Blender 4.4 的 Python API 无头生成品牌页的母舰资产，并烘焙法线 / 环境光遮蔽 / 粗糙度贴图后导出 glb。命令按顺序执行子命令（`--` 之后的参数）：

```
B=/Applications/Blender.app/Contents/MacOS/Blender
$B -b -P tools/blender/axiom_ship.py -- build render v1 bake export web/public/models/axiom.glb
```

子命令：`build` 建模；`render <tag>` 输出 `render-<tag>*.png`（含船头、船尾、俯视）；`bake` / `bake_fast` 烘焙贴图；`export <path>` 导出 GLB；`save <file>` / `open <file>` 保存或打开中间 .blend。中间文件都写在本目录，已被忽略提交。

压缩（可选）：

```
npx @gltf-transform/cli optimize axiom.glb axiom-opt.glb --compress meshopt --texture-compress webp
```
