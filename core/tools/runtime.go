package tools

import "context"

// Runtime 描述一次工具调用所绑定的任务级运行时信息。
type Runtime struct {
	TaskID string
	StepID string
	Actor  string

	Metadata map[string]string
}

type runtimeContextKey struct{}

// WithRuntime 将工具运行时信息写入上下文，供下游 handler 读取。
func WithRuntime(ctx context.Context, runtime *Runtime) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if runtime == nil {
		return ctx
	}
	return context.WithValue(ctx, runtimeContextKey{}, runtime)
}

// RuntimeFromContext 从上下文中提取工具运行时信息。
func RuntimeFromContext(ctx context.Context) (*Runtime, bool) {
	if ctx == nil {
		return nil, false
	}
	runtime, ok := ctx.Value(runtimeContextKey{}).(*Runtime)
	if !ok || runtime == nil {
		return nil, false
	}
	return runtime, true
}
