package main

import "testing"

// 说话人标注最容易错的地方不是聚类，而是「把说话人区间贴到转写段落上」这一步：
// ASR 段落和 diar 区间的边界永远对不齐，重叠判定太松就会张冠李戴。
func TestAssignSpeakersNeedsEnoughOverlap(t *testing.T) {
	segs := []Segment{
		{Start: 0, End: 5, Text: "我先说一下这周的进度"},
		{Start: 5, End: 9, Text: "那我补充两点"},
	}
	ds := []DiarSeg{
		{Start: 0, End: 4.9, Speaker: 0},
		// 只有 0.1 秒落进第一段，是别人的插话，不该把第一段判给说话人2
		{Start: 4.9, End: 9, Speaker: 1},
	}
	n := assignSpeakers(segs, ds)
	if segs[0].Speaker != 1 {
		t.Errorf("第一段应归说话人1，实际 %d", segs[0].Speaker)
	}
	if segs[1].Speaker != 2 {
		t.Errorf("第二段应归说话人2，实际 %d", segs[1].Speaker)
	}
	if n != 2 {
		t.Errorf("应识别出 2 个说话人，实际 %d", n)
	}
}

func TestAssignSpeakersShortSegment(t *testing.T) {
	// 很短的段落（0.4 秒）重叠 0.15 秒就够了，不能被 0.3 的绝对门槛挡掉
	segs := []Segment{{Start: 10, End: 10.4, Text: "对"}}
	ds := []DiarSeg{{Start: 10.1, End: 11, Speaker: 2}}
	assignSpeakers(segs, ds)
	if segs[0].Speaker != 3 {
		t.Errorf("短段落应该按 25%% 相对门槛判定，实际 %d", segs[0].Speaker)
	}
}

func TestAssignSpeakersNoOverlap(t *testing.T) {
	segs := []Segment{{Start: 20, End: 24, Text: "这段没有对应的说话人区间"}}
	ds := []DiarSeg{{Start: 0, End: 10, Speaker: 0}}
	n := assignSpeakers(segs, ds)
	if segs[0].Speaker != 0 {
		t.Errorf("没有重叠就不该标注，实际 %d", segs[0].Speaker)
	}
	if n != 0 {
		t.Errorf("不该统计出说话人，实际 %d", n)
	}
}

func TestAssignSpeakersPicksDominant(t *testing.T) {
	// 段落里两个人都有话，谁占比大归谁
	segs := []Segment{{Start: 0, End: 6, Text: "你一句我一句"}}
	ds := []DiarSeg{
		{Start: 0, End: 4.5, Speaker: 0},
		{Start: 4.5, End: 6, Speaker: 1},
	}
	assignSpeakers(segs, ds)
	if segs[0].Speaker != 1 {
		t.Errorf("应归占比大的说话人1，实际 %d", segs[0].Speaker)
	}
}
