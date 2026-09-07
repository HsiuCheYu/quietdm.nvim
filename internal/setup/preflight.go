package setup

import (
	"context"
	"fmt"
	"os/exec"
)

// preflight checks that the tools quietdmd setup needs are actually
// available, so a missing Docker install fails in one clear line instead of
// wherever the first `docker compose` call happens to be.
func preflight(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in PATH — see docs/self-host.md \"〇、需要的東西\"")
	}
	if err := exec.CommandContext(ctx, "docker", "compose", "version").Run(); err != nil {
		return fmt.Errorf("docker compose not available: %w — see docs/self-host.md \"〇、需要的東西\"", err)
	}
	return nil
}
