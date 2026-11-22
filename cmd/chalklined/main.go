// chalklined 是 Chalkline 唯一后端进程：REST + MCP 两个入口共享同一套领域与事务边界。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/yuylong/Chalkline/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "chalkline: %v\n", err)
		os.Exit(1)
	}
}
