package platform

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Runner interface {
	Run(context.Context, string, ...string) (string, error)
}
type Exec struct{}
type limitedOutput struct{ strings.Builder }

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := (4 << 20) - b.Len(); remaining > 0 {
		b.Builder.Write(p[:min(remaining, len(p))])
	}
	return n, nil
}
func (Exec) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	var out limitedOutput
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("%s: %w\n%s", name, err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}
