package main

import "testing"

// 纠错的解析必须严格：模型少一行、多一行、序号乱了，都要整批丢弃。
// 宁可保留错字，也不能让文本错位——会议记录串行比错别字严重得多。
func TestParseCorrectedOK(t *testing.T) {
	out := "1|这次会议的结论是。\n2|登录页改版本周五上线。\n3|付费转化的埋点，请小李在周四之前补完。"
	got, ok := parseCorrected(out, 3)
	if !ok {
		t.Fatal("正常输出应该解析成功")
	}
	want := []string{"这次会议的结论是。", "登录页改版本周五上线。", "付费转化的埋点，请小李在周四之前补完。"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 行应为 %q，实际 %q", i+1, want[i], got[i])
		}
	}
}

func TestParseCorrectedToleratesFormatting(t *testing.T) {
	// 小模型爱加代码块、列表符号、全角分隔符
	out := "```\n1｜登录页改版\n- 2|付费转化的埋点\n3|“埋点”\n```"
	got, ok := parseCorrected(out, 3)
	if !ok {
		t.Fatal("应该容忍常见格式噪声")
	}
	if got[0] != "登录页改版" || got[1] != "付费转化的埋点" {
		t.Errorf("解析结果不对: %#v", got)
	}
	if got[2] != "埋点" {
		t.Errorf("应该去掉模型加的中文引号，实际 %q", got[2])
	}
}

func TestParseCorrectedRejectsMissingLine(t *testing.T) {
	// 少一行必须整批拒绝，否则后面的文本会整体错位
	out := "1|第一行\n3|第三行"
	if _, ok := parseCorrected(out, 3); ok {
		t.Error("缺行时不该判为成功")
	}
}

func TestParseCorrectedRejectsExtraLine(t *testing.T) {
	out := "1|第一行\n2|第二行\n3|第三行\n4|多出来的"
	got, ok := parseCorrected(out, 3)
	if !ok {
		t.Fatal("多出的行号超出范围应被忽略，其余仍可用")
	}
	if len(got) != 3 || got[2] != "第三行" {
		t.Errorf("结果不对: %#v", got)
	}
}

func TestParseCorrectedIgnoresGarbage(t *testing.T) {
	out := "好的，我来修正：\n1|第一行\n2|第二行\n3|第三行\n以上是修正结果。"
	got, ok := parseCorrected(out, 3)
	if !ok {
		t.Fatal("应该跳过不带序号的说明文字")
	}
	if got[0] != "第一行" {
		t.Errorf("结果不对: %#v", got)
	}
}
