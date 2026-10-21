# -*- coding: utf-8 -*-
"""本地翻译（MarianMT / Helsinki-NLP opus-mt），纯离线、CPU 可跑。

两种用法：
  单次：translate.py --dir en2zh --in in.json --out out.json
        输入 {"texts": ["..."]}，输出 {"texts": ["..."], "seconds": 1.2}
  常驻：translate.py --serve
        stdin 每行一个 {"id":1,"dir":"en2zh","texts":[...]}
        stdout 每行一个 {"id":1,"texts":[...],"seconds":...}
"""

import argparse
import json
import os
import re
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_MODELS = os.path.join(HERE, "models", "mt")
GLOSSARY_FILE = os.path.join(HERE, "glossary.json")

_MODELS = {}

# 需要原样保留的技术名词（专有名词，直译会变成「变形器」「关注」这种）
BUILTIN_TERMS = [
    "transformer", "deepseek", "chatgpt", "openai", "claude", "qwen", "llama",
    "python", "javascript", "typescript", "docker", "kubernetes", "linux", "github",
    "pytorch", "tensorflow", "onnx", "cuda", "huggingface", "modelscope",
    "whisper", "sensevoice", "gpt-sovits", "ffmpeg", "webassembly",
]


def load_terms():
    terms = list(BUILTIN_TERMS)
    if os.path.exists(GLOSSARY_FILE):
        try:
            extra = json.load(open(GLOSSARY_FILE, encoding="utf-8"))
            if isinstance(extra, list):
                terms.extend([str(x) for x in extra if str(x).strip()])
            elif isinstance(extra, dict):
                terms.extend([str(k) for k in extra.keys()])
        except Exception:
            pass
    # 长词优先，避免 "gpt" 抢先匹配 "gpt-sovits"
    return sorted(set(t.lower() for t in terms), key=len, reverse=True)


_TERMS = load_terms()

# 保护对象：函数式记号 O(n)、全大写缩写（API/GPU/LLM）、驼峰词（OpenAI）、术语表
# 注意：缩写与驼峰必须大小写敏感——加了 IGNORECASE 后 [A-Z]{2,} 会匹配所有单词。
_PROTECT = re.compile(
    r"(O\([^)]*\)"                          # 复杂度记号
    r"|(?-i:\b[A-Z]{2,}[0-9]*\b)"           # API / GPU / GPT4
    r"|(?-i:\b[a-z]+[A-Z][A-Za-z0-9]*\b)"   # OpenAI / JavaScript
    r"|\b(?:" + "|".join(re.escape(t) for t in _TERMS) + r")\b)",
    re.IGNORECASE,
)


def mask_terms(text):
    """把技术名词换成 Z0Z/Z1Z 这类占位符，返回 (掩码文本, 映射表)"""
    store = {}

    def repl(m):
        token = m.group(0)
        key = "Q%dP" % len(store)
        store[key] = token
        # 两侧留空格；即使被模型粘成 Q0PQ1P，还原时也能用正则分别匹配
        return " " + key + " "

    return _PROTECT.sub(repl, text), store


def tidy_token(t):
    """还原数学记号：O(n squared) → O(n²)"""
    t = re.sub(r"([A-Za-z0-9])\s+squared", r"\1²", t)
    t = re.sub(r"([A-Za-z0-9])\s+cubed", r"\1³", t)
    return t


def unmask_terms(text, store):
    if not store:
        return text

    def repl(m):
        key = "Q%sP" % m.group(1)
        term = store.get(key)
        return tidy_token(term) if term else m.group(0)

    text = re.sub(r"Q\s*(\d+)\s*P", repl, text)
    text = re.sub(r"[ \t]{2,}", " ", text).strip()
    return text


def load_model(models_dir, direction):
    """按方向加载（并缓存）翻译模型。direction: en2zh / zh2en"""
    if direction in _MODELS:
        return _MODELS[direction]

    import torch
    from transformers import MarianMTModel, MarianTokenizer

    folder = os.path.join(models_dir, {"en2zh": "en-zh", "zh2en": "zh-en"}[direction])
    if not os.path.isdir(folder):
        raise RuntimeError("缺少翻译模型目录：%s" % folder)

    tok = MarianTokenizer.from_pretrained(folder, local_files_only=True)
    model = MarianMTModel.from_pretrained(folder, local_files_only=True)
    model.eval()
    try:
        torch.set_num_threads(max(2, min(8, os.cpu_count() or 4)))
    except Exception:
        pass
    _MODELS[direction] = (tok, model)
    return tok, model


def translate(models_dir, direction, texts):
    if not texts:
        return []
    import torch

    tok, model = load_model(models_dir, direction)

    # 先做术语保护，再翻译，最后还原
    masked, stores = [], []
    for t in texts:
        m, store = mask_terms(t)
        masked.append(m)
        stores.append(store)

    out = []
    batch = 8
    for i in range(0, len(masked), batch):
        chunk = [t if t.strip() else "。" for t in masked[i:i + batch]]
        enc = tok(chunk, return_tensors="pt", padding=True, truncation=True, max_length=512)
        with torch.no_grad():
            gen = model.generate(**enc, max_length=512, num_beams=4)
        out.extend(tok.batch_decode(gen, skip_special_tokens=True))

    # 空输入原样返回空串，避免把占位符译出来
    res = ["" if not texts[i].strip() else unmask_terms(out[i], stores[i]) for i in range(len(texts))]
    if direction == "en2zh":
        # 中文标点前不要留空格（占位符两侧补的空格会残留）
        res = [re.sub(r"[ \t]+([。，、；：！？])", r"\1", s) for s in res]
        res = [re.sub(r"([。，、；：！？])[ \t]+", r"\1", s) for s in res]
    return res


def handle(req, models_dir):
    t0 = time.time()
    direction = req.get("dir", "en2zh")
    if direction not in ("en2zh", "zh2en"):
        raise RuntimeError("不支持的方向：%s" % direction)
    res = translate(models_dir, direction, req.get("texts") or [])
    return {"id": req.get("id", 0), "texts": res, "seconds": round(time.time() - t0, 2)}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--models", default=DEFAULT_MODELS)
    ap.add_argument("--dir", default="en2zh", choices=["en2zh", "zh2en"])
    ap.add_argument("--in", dest="inp", default="")
    ap.add_argument("--out", dest="out", default="")
    ap.add_argument("--serve", action="store_true")
    args = ap.parse_args()

    if args.serve:
        for line in sys.stdin:
            line = line.strip()
            if not line:
                continue
            try:
                req = json.loads(line)
                resp = handle(req, args.models)
            except Exception as e:  # 出错也要回一行，否则调用方会一直等
                resp = {"id": 0, "error": str(e), "texts": []}
            sys.stdout.write(json.dumps(resp, ensure_ascii=False) + "\n")
            sys.stdout.flush()
        return

    data = json.load(open(args.inp, encoding="utf-8"))
    resp = handle({"dir": args.dir, "texts": data.get("texts") or []}, args.models)
    if args.out:
        json.dump(resp, open(args.out, "w", encoding="utf-8"), ensure_ascii=False)
    else:
        print(json.dumps(resp, ensure_ascii=False))


if __name__ == "__main__":
    main()
