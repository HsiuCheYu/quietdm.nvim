package setup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// dockerCompose runs `docker compose <args...>` in dir, streaming its output
// live the way `docker compose logs -f` would — the self-host doc's advice to
// "watch it say listening" only works if the wizard shows the same output.
func dockerCompose(ctx context.Context, dir string, out, errOut io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = errOut
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// waitHTTPReady polls url until it answers (any status code — the homeserver
// merely needs to be listening) or timeout elapses. It replaces "等它說
// listening" from docs/self-host.md with something the wizard can check
// itself.
func waitHTTPReady(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		if resp, err := doGet(ctx, client, url); err == nil {
			resp.Body.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s to answer", url)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func doGet(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}
