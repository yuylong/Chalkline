// chalklined 是 Chalkline 唯一后端进程：HTTP API（M1 起含 REST 与 MCP，M0 为骨架端点）。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/yuylong/Chalkline/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "chalkline: %v\n", err)
		os.Exit(1)
	}
}
