package service

import (
	"fmt"
	"os"
	"path/filepath"
)

// StartCmd 命令行命令定义
type StartCmd struct {
	Name    string
	Comment string
	Func    func()
}

var startCmds = []StartCmd{
	{"start", "Start server", Start},
}

// AddCmd 添加自定义命令行命令
func AddCmd(name, comment string, function func()) {
	startCmds = append(startCmds, StartCmd{name, comment, function})
}

// CheckCmd 检查并执行命令行命令
func CheckCmd() {
	if len(os.Args) > 1 {
		cmd := os.Args[1]
		if cmd == "help" || cmd == "--help" {
			showHelp()
			os.Exit(0)
		}

		for _, cmdInfo := range startCmds {
			if cmd == cmdInfo.Name {
				cmdInfo.Func()
				os.Exit(0)
			}
		}
	}
}

func showHelp() {
	fmt.Printf("Usage: %s [command]\n\n", filepath.Base(os.Args[0]))
	fmt.Println("Available commands:")
	for _, cmdInfo := range startCmds {
		fmt.Printf("  %-10s %s\n", cmdInfo.Name, cmdInfo.Comment)
	}
}
