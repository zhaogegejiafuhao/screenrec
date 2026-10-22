package main

import "testing"

func TestParseActionsChineseCommaTail(t *testing.T) {
	md := "## 待办\n- [ ] 补全付费转化埋点，小李负责，周四前\n- [ ] 整理接口文档，刘芳\n- [ ] 剩下的用例补完，不然提测要延\n- [ ] 出压测报告 — 张伟 / 下周一\n"
	got := parseActions(md)
	for _, a := range got {
		t.Logf("text=%q owner=%q due=%q", a.Text, a.Owner, a.Due)
	}
	if len(got) != 4 {
		t.Fatalf("应解析出 4 条，实际 %d", len(got))
	}
	if got[0].Text != "补全付费转化埋点" || got[0].Owner != "小李" || got[0].Due == "" {
		t.Errorf("第一条元数据没摘干净：%+v", got[0])
	}
	// 纯人名尾巴（"整理接口文档，刘芳"）没有时间词也没有"负责"，判不准就不切，
	// 保持任务完整、负责人交给 guessOwner 去猜，避免把正文切残
	if got[1].Text != "整理接口文档，刘芳" {
		t.Errorf("第二条不该被切：%+v", got[1])
	}
	// 「不然提测要延」是任务内容不是元数据，不能切
	if got[2].Text != "剩下的用例补完，不然提测要延" {
		t.Errorf("第三条被误切了：%+v", got[2])
	}
	if got[3].Owner != "张伟" || got[3].Due != "下周一" {
		t.Errorf("老格式回归失败：%+v", got[3])
	}
}
