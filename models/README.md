# 模型目录

模型权重不随仓库分发（总计约 1.1GB）。用脚本一次拉齐：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/fetch-models.ps1
```

下载完成后的目录结构：

```
models/
├── sensevoice/
│   ├── model.int8.onnx          # 239MB  整段转写
│   └── tokens.txt
├── stream/
│   ├── encoder.int8.onnx        # 227MB  实时字幕
│   ├── decoder.int8.onnx
│   └── tokens.txt
├── silero_vad.onnx              # 2MB    端点检测
├── diar/
│   ├── segmentation.int8.onnx   # 1.5MB  说话人分段
│   └── embedding.onnx           # 40MB   声纹嵌入
└── mt/
    ├── en-zh/                   # 301MB  英译中（可选）
    └── zh-en/                   # 301MB  中译英（可选）
```

只做转写的话，`mt/` 可以不装；只做中文会议的话，`stream/` 也可以跳过（会退化为分块识别）。

各模型的许可证见 `THIRD_PARTY.md`。
