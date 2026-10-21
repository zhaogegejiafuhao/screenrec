// 口语数学公式规范化 CLI：读参数或标准输入，输出符号形式。
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"screenrec/internal/mathnorm"
)

func main() {
	var in string
	if len(os.Args) > 1 {
		in = strings.Join(os.Args[1:], " ")
	} else {
		b, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		in = b
	}
	fmt.Println(mathnorm.Normalize(strings.TrimSpace(in)))
}
