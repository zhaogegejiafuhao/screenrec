// Package mathnorm 把口语数学表达规范化为符号形式。
// 例如「X的平方加Y的平方等于C的平方」→「x² + y² = c²」。
// 只做规则替换，不改变非公式句子的语义。
package mathnorm

import (
	"regexp"
	"strings"
	"unicode"
)

// Normalize 把口语化的数学表达转成符号表达；非公式句子原样返回
func Normalize(s string) string {
	if !hasCJK(s) {
		if looksMathEn(s) {
			return normEnglish(s)
		}
		// 普通英文原样返回：lowerSingleLetters 会把代词 "I" 变成 "i"
		return s
	}
	if !looksMath(s) {
		return lowerSingleLetters(s)
	}

	// 句内句号会把一个公式拆成两句，这里先按句处理再拼回
	parts := splitSentences(s)
	for i, p := range parts {
		parts[i] = normOne(p)
	}
	return strings.Join(parts, "")
}

var (
	reFraction = regexp.MustCompile(`([一二三四五六七八九十两\d]+)分之([^\s，。；、,.;]+)`)
	reSqrt     = regexp.MustCompile(`根号\s*([A-Za-z0-9π]+|派)`)
	reSquare   = regexp.MustCompile(`([A-Za-z0-9\)\]π]+)的平方`)
	reCube     = regexp.MustCompile(`([A-Za-z0-9\)\]π]+)的立方`)
	reNegSq    = regexp.MustCompile(`([A-Za-z0-9π]+)的负\s*([A-Za-z0-9π]+)\s*平方次方`)
	reNegPow   = regexp.MustCompile(`([A-Za-z0-9\)\]π]+)的负\s*([^的，。]+?)\s*次方`)
	rePow      = regexp.MustCompile(`([A-Za-z0-9\)\]π]+)的\s*([A-Za-z0-9π]+)\s*次方`)
	reIntegral = regexp.MustCompile(`积分(?:从)?\s*([^\s到，。]+?)\s*到\s*([^\s，。]+)`)
	reDiff     = regexp.MustCompile(`\bd([XYZ])\b`) // 微元 dX → dx
	reSpaces   = regexp.MustCompile(`[ \t]{2,}`)
)

var cnNum = map[string]string{
	"一": "1", "二": "2", "两": "2", "三": "3", "四": "4", "五": "5",
	"六": "6", "七": "7", "八": "8", "九": "9", "十": "10",
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func looksMath(s string) bool {
	// 强信号：出现这些词基本可以确定是公式朗读
	for _, k := range []string{"平方", "立方", "次方", "根号", "分之", "积分", "求和", "极限", "约等于", "正弦", "余弦", "对数"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	// 弱信号：只有「等于」不够（「这等于白做」），必须同时有单字母变量
	if strings.Contains(s, "等于") && hasSingleLatin(s) {
		return true
	}
	return false
}

func looksMathEn(s string) bool {
	l := strings.ToLower(s)
	for _, k := range []string{"squared", "cubed", "integral", "equals", "is equal to", "to the power of", "square root", "divided by"} {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}

// hasSingleLatin 判断是否存在孤立单字母（x/y/z 这类变量），用于区分公式与口语比喻
func hasSingleLatin(s string) bool {
	rs := []rune(s)
	isL := func(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }
	for i, r := range rs {
		if !isL(r) {
			continue
		}
		var prev, next rune
		if i > 0 {
			prev = rs[i-1]
		}
		if i+1 < len(rs) {
			next = rs[i+1]
		}
		if !isL(prev) && !isL(next) {
			return true
		}
	}
	return false
}

func splitSentences(s string) []string {
	var out []string
	var cur []rune
	for _, r := range s {
		cur = append(cur, r)
		if r == '。' || r == '！' || r == '？' || r == '.' || r == ';' || r == '；' {
			out = append(out, string(cur))
			cur = nil
		}
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

func normOne(s string) string {
	// 0) 复杂度写法：O(n^2) 常被识别成「ON的平方」
	s = strings.ReplaceAll(s, "ON的平方", "O(n²)")
	s = strings.ReplaceAll(s, "O(n)的平方", "O(n²)")

	// 1) 根号（先于分数，才能正确切出「二分之根号派」）
	s = reSqrt.ReplaceAllStringFunc(s, func(m string) string {
		sub := reSqrt.FindStringSubmatch(m)
		inner := sub[1]
		if inner == "派" {
			inner = "π"
		}
		return "√" + inner
	})

	// 2) 分数：二分之根号派 → √π/2
	s = reFraction.ReplaceAllStringFunc(s, func(m string) string {
		sub := reFraction.FindStringSubmatch(m)
		den := sub[1]
		if v, ok := cnNum[den]; ok {
			den = v
		}
		return sub[2] + "/" + den
	})

	// 3) 幂：先处理「的负 X 平方次方」这种嵌套，再平方/立方，再负次方，最后普通次方
	s = reNegSq.ReplaceAllString(s, "$1^(−$2²)")
	s = reSquare.ReplaceAllString(s, "$1²")
	s = reCube.ReplaceAllString(s, "$1³")
	s = reNegPow.ReplaceAllString(s, "$1^(−$2)")
	s = rePow.ReplaceAllString(s, "$1^$2")

	// 4) 积分：积分从零到无穷大 → ∫₀^∞（上下标里的中文数字要先换算）
	s = reIntegral.ReplaceAllStringFunc(s, func(m string) string {
		sub := reIntegral.FindStringSubmatch(m)
		lo := strings.NewReplacer("零", "0", "无穷大", "∞", "无穷", "∞").Replace(sub[1])
		hi := strings.NewReplacer("零", "0", "无穷大", "∞", "无穷", "∞").Replace(sub[2])
		if lo == "0" && hi == "∞" {
			return "∫₀^∞"
		}
		return "∫_" + lo + "^" + hi
	})

	// 5) 运算符与常数（顺序敏感：长的先替换）
	s = replaceAllOrdered(s, [][2]string{
		{"大于等于", " ≥ "}, {"小于等于", " ≤ "}, {"不等于", " ≠ "}, {"约等于", " ≈ "},
		{"大于", " > "}, {"小于", " < "},
		{"等于", " = "},
		{"加上", " + "}, {"再加", " + "}, {"加", " + "},
		{"减去", " − "}, {"减", " − "},
		{"乘以", " × "}, {"乘上", " × "}, {"乘", " × "},
		{"除以", " ÷ "},
		{"无穷大", "∞"}, {"派", "π"}, {"零", "0"},
	})

	s = lowerSingleLetters(s)
	s = reDiff.ReplaceAllStringFunc(s, func(m string) string {
		return "d" + strings.ToLower(m[1:])
	}) // dX → dx
	return cleanSpaces(s)
}

var (
	reEnIntegral = regexp.MustCompile(`integral from ([a-z0-9]+) to ([a-z0-9]+) of`)
	reEnSquared  = regexp.MustCompile(`([a-z0-9]) squared`)
	reEnCubed    = regexp.MustCompile(`([a-z0-9]) cubed`)
	reEnNegPow   = regexp.MustCompile(`to the power of minus ([a-z0-9²³]+)`)
	reEnPow      = regexp.MustCompile(`to the power of ([a-z0-9²³]+)`)
	reEnRoot     = regexp.MustCompile(`(?:square )?root (?:of )?([a-z0-9π]+)`)
	reSpacePow   = regexp.MustCompile(`\s+\^`)
	reEnLeadThe  = regexp.MustCompile(`(^|[.!?]\s*)the `)
)

// normEnglish 英文口语公式的粗略规范化。英文语序和中文不同，
// 但「integral from A to B of <被积式> dx equals ...」恰好就是公式顺序，直接替换即可。
func normEnglish(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	t = reEnLeadThe.ReplaceAllString(t, "$1") // 去掉句首/句间的 "the"

	t = reEnIntegral.ReplaceAllString(t, "∫_$1^$2 ")
	t = reEnSquared.ReplaceAllString(t, "$1²")
	t = reEnCubed.ReplaceAllString(t, "$1³")
	t = reEnNegPow.ReplaceAllString(t, "^(−$1)")
	t = reEnPow.ReplaceAllString(t, "^$1")
	t = strings.ReplaceAll(t, "root pi", "√π")
	t = reEnRoot.ReplaceAllString(t, "√$1")

	t = replaceAllOrdered(t, [][2]string{
		{"is equal to", "="}, {"equals", "="},
		{"divided by", "/"}, {"multiplied by", "×"},
		{" plus ", " + "}, {" minus ", " − "}, {" times ", " × "}, {" over ", " / "},
		{"infinity", "∞"}, {"zero", "0"},
		{"d x", "dx"}, {"d y", "dy"},
	})
	t = reSpacePow.ReplaceAllString(t, "^")    // e ^x → e^x
	t = strings.ReplaceAll(t, "∫_0^∞", "∫₀^∞") // 统一的积分记号
	return cleanSpaces(t)
}

func replaceAllOrdered(s string, pairs [][2]string) string {
	for _, p := range pairs {
		s = strings.ReplaceAll(s, p[0], p[1])
	}
	return s
}

// lowerSingleLetters 把孤立的单个大写字母转小写（x/y/z 这类变量），
// 不动单词里的大写字母，也不动 Transformer 这类专有名词。
func lowerSingleLetters(s string) string {
	rs := []rune(s)
	isLetter := func(r rune) bool { return unicode.IsLetter(r) && r < 128 }
	for i, r := range rs {
		if r < 'A' || r > 'Z' {
			continue
		}
		var prev, next rune
		if i > 0 {
			prev = rs[i-1]
		}
		if i+1 < len(rs) {
			next = rs[i+1]
		}
		if !isLetter(prev) && !isLetter(next) && next != '(' && prev != '(' {
			rs[i] = unicode.ToLower(r)
		}
	}
	return string(rs)
}

// cleanSpaces 合并多余空格
func cleanSpaces(s string) string {
	s = reSpaces.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "  ", " ")
	return s
}
