package tool

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// TimeToolInput is the input type for getCurrentTime (no parameters).
type TimeToolInput struct{}

// NewTimeTool creates a tool that returns the current time in Asia/Shanghai.
// Mirrors Java TimeTool — @Tool("getCurrentTime"), format: "yyyy-MM-dd HH:mm:ss EEEE (中国标准时间)"
func NewTimeTool() (tool.InvokableTool, error) {
	return utils.InferTool[TimeToolInput, string](
		"getCurrentTime",
		"获取当前时间，返回中国标准时间格式。当用户询问当前时间、日期、星期几等信息时调用此工具。",
		func(ctx context.Context, _ TimeToolInput) (string, error) {
			loc, err := time.LoadLocation("Asia/Shanghai")
			if err != nil {
				return "", fmt.Errorf("failed to load timezone: %w", err)
			}

			now := time.Now().In(loc)

			// Go weekday names are English; map to Chinese weekdays
			weekdays := map[time.Weekday]string{
				time.Sunday:    "星期日",
				time.Monday:    "星期一",
				time.Tuesday:   "星期二",
				time.Wednesday: "星期三",
				time.Thursday:  "星期四",
				time.Friday:    "星期五",
				time.Saturday:   "星期六",
			}

			return fmt.Sprintf("%s %s (中国标准时间)",
				now.Format("2006-01-02 15:04:05"),
				weekdays[now.Weekday()]), nil
		},
	)
}