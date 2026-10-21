// 测试用：模拟 OpenAI 兼容接口，返回固定纪要
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const sample = `## 一句话总结
团队确认先排查 API 延迟问题，并补齐公式整理规则。

## 议题
- API 延迟偏高
- 数学公式的识别与整理

## 结论
- 优先检查 GPU 与推理管线，再决定是否扩容

## 待办
- [ ] 检查 GPU 使用率与推理耗时 — 说话人2 / 本周内
- [ ] 补充公式整理规则 — 未指定 / 未指定

## 风险与分歧
- 原文未提及明确的上线时间`

func main() {
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Printf("mock 收到 %d 字节, auth=%q\n", len(body), r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": sample}},
			},
		})
	})
	fmt.Println("mock summary listening on 127.0.0.1:8899")
	http.ListenAndServe("127.0.0.1:8899", nil)
}
