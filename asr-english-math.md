# 英文识别与数学公式：实测结论

测试环境：SenseVoice INT8（本地 CPU 推理）、流式 Paraformer 双语模型、RTX 3050 笔记本。
样本用 Windows SAPI 合成（英文 Microsoft Zira / 中文 Microsoft Huihui），避免人声录音的偶然性。

## 一、英文：可以用，而且不用改任何配置

模型本身支持中/英/日/韩/粤，`language="auto"` 自动判别，界面上不需要选语言。

| 样本 | 时长 | 识别结果 | 评价 |
|---|---|---|---|
| 自然语句 | 5.97s | The meeting will start at three tomorrow afternoon. Prepare the quarterly report. | **一字不差** |
| 数学朗读 | 10.13s | X squared plus y squared equals c squared. The integral from 0 to infinity of E to the power of minus x squared D x equals root pi over 2. | 仅 `z`→`c` 一处错 |

推理耗时 0.24~0.43 秒，RTF ≈ 0.04（25 倍实时）。

**注意两个模型的差别**：

- 整段转写（SenseVoice）：准确、带标点，是最终结果
- 实时字幕（流式 Paraformer）：`at three` 会听成 `ted three`，无大小写、无标点

产品里断句后会用 SenseVoice 重新解码这一句覆盖草稿，所以最终文本以精校为准；实时那几秒的草稿英文会粗糙一些。

## 二、数学公式：识别没问题，但「转译成公式」是另一回事

**必须说清楚**：语音识别输出的是「人说的话」，不是公式符号。它听到「X 的平方」，只会写「X的平方」，不会自己变成 `x²`。

实测原始输出：

```
X的平方加Y的平方等于C的平方。积分从零到无穷大。E的负X平方次方dX等于2分之根号派。
```

这句话里的信息其实是对的（除了 z→c），只是形态是口语。加一层规则整理后：

```
x² + y² = c²。∫₀^∞。e^(−x²)dx = √π/2。
```

英文同样：

```
原始：X squared plus y squared equals c squared. The integral from 0 to infinity of ...
整理：x² + y² = c². ∫₀^∞ e^(−x²) dx = √π / 2.
```

代码与算法复杂度也能整理：

```
原始：注意力机制的复杂度是ON的平方
整理：注意力机制的复杂度是O(n²)
```

## 三、规则层覆盖了什么

工具：`tools/mathnorm`（纯 Go 标准库，无第三方依赖）

| 能力 | 例子 |
|---|---|
| 幂 | 平方→²、立方→³、`的负X平方次方`→`^(−x²)`、`的N次方`→`^n` |
| 根号 / 常数 | 根号派→√π、派→π |
| 分数 | 二分之根号派→√π/2 |
| 积分 | 积分从零到无穷大→∫₀^∞ |
| 运算符 | 加/减/乘/除以/等于/大于等于/约等于 |
| 微元 | dX→dx |
| 单字母变量 | `X`→`x`，但不动 `Transformer` 这类单词 |
| 复杂度 | `ON的平方`→`O(n²)` |
| 防误触发 | 「这个方案等于白做」原样保留（只有「等于」不足以判定公式） |

## 四、规则层做不到的

1. **字母混淆**：`z` 被听成 `c`（中英都出现）。这是声学模型的单字母识别问题，规则无从判断，只能靠上下文或大模型纠正。
2. **粘连**：`O n 的平方` 会变成 `ON`，需要专门规则打补丁；更多变量组合（`a i`、`x i j`）无法穷举。
3. **复杂结构**：矩阵、多行推导、分段函数、上下标嵌套（`a_{i,j}^{(k)}`）、求和上下限，规则会写成一堆特例且容易错。
4. **符号读法**：`∑` 念「西格玛 i 从 1 到 n」、`∂` 念「偏导」、`∈` 念「属于」——要覆盖这些得建一套口语数学词典。
5. **公式与正文混排**：一句话里既有推导又有解释时，规则会过度替换。

## 五、实用建议

- **只需要可读文本**（会议、访谈、课程笔记）：现在的识别就够，英文直接可用，不需要任何设置。
- **需要能复制的公式**（数学课、算法课）：先用 `mathnorm` 把口语整理成符号；简单公式（幂、根号、分数、积分、复杂度）已经够用。
- **需要 LaTeX / 可编辑公式**：把「转写文本」丢给大模型做结构化（提示词要求输出 LaTeX），这一步本地 4GB 显存跑不动，需要 API。规则层可以放在它前面，先把口语规范化，降低模型出错率。

## 六、复现方式

```bash
# 生成样本（需要 Windows 语音）
go run ./tools/tts en "x squared plus y squared equals c squared." test_audio/a.wav
go run ./tools/tts zh "x 的平方加 y 的平方等于 z 的平方。" test_audio/b.wav

# 识别
python asr.py test_audio/b.wav out.json

# 公式整理
go run ./tools/mathnorm "X的平方加Y的平方等于C的平方。"
```
