// todo_helper 是终端待办工具，客户端与服务端共用一个二进制。
//
// 作者: wjchen
package main

import (
	"os"

	"todo_helper/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
