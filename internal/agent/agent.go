package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gatelens/gatelens/internal/domain"
	"github.com/gatelens/gatelens/internal/source"
)

type Config struct {
	ServerURL   string
	Token       string
	ClusterID   string
	ClusterName string
	ClusterRole string
	Interval    time.Duration
}

type Runner struct {
	reader source.Reader
	config Config
	client *http.Client
}

func New(reader source.Reader, config Config) (*Runner, error) {
	config.ServerURL = strings.TrimRight(strings.TrimSpace(config.ServerURL), "/")
	if config.ServerURL == "" {
		return nil, fmt.Errorf("server URL is required")
	}
	if config.ClusterID == "" {
		return nil, fmt.Errorf("cluster ID is required")
	}
	if config.ClusterName == "" {
		config.ClusterName = config.ClusterID
	}
	if config.ClusterRole == "" {
		config.ClusterRole = "member"
	}
	if config.Interval <= 0 {
		config.Interval = 30 * time.Second
	}
	return &Runner{reader: reader, config: config, client: &http.Client{Timeout: 15 * time.Second}}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	commandsStarted := false
	for {
		delay := r.config.Interval
		err := r.SendOnce(ctx)
		if err == nil && !commandsStarted {
			commandsStarted = true
			go r.runCommands(ctx)
		} else if errors.Is(err, errLocalSnapshotNotReady) {
			delay = snapshotReadyRetryDelay
		} else if err != nil && ctx.Err() == nil {
			fmt.Printf("GateLens agent snapshot upload failed: %v\n", err)
		}
		if !waitForRetry(ctx, delay) {
			return nil
		}
	}
}

var errLocalSnapshotNotReady = errors.New("local snapshot is not ready")

func (r *Runner) SendOnce(ctx context.Context) error {
	localContext := r.reader.Context()
	topology := r.reader.Topology()
	if topology.SnapshotID == "" || localContext.Snapshot.ID == "" {
		return errLocalSnapshotNotReady
	}
	cluster := domain.TopologyCluster{ID: r.config.ClusterID, Name: r.config.ClusterName, Role: r.config.ClusterRole, Version: localContext.Cluster.Version, ConnectionState: "connected", Namespaces: append([]string(nil), localContext.Namespaces...), Snapshot: localContext.Snapshot}
	payload := domain.AgentSnapshot{Cluster: cluster, Context: localContext, Topology: topology, Findings: r.reader.Findings(), Resources: r.reader.Resources(""), SentAt: time.Now().UTC().Format(time.RFC3339)}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.config.ServerURL+"/api/v1/agent/snapshots", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create snapshot request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if r.config.Token != "" {
		request.Header.Set("Authorization", "Bearer "+r.config.Token)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("upload snapshot: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("snapshot server returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	return nil
}

const (
	snapshotReadyRetryDelay  = 2 * time.Second
	commandRetryDelay        = 2 * time.Second
	maxCommandPollRetryDelay = 30 * time.Second
)

func (r *Runner) runCommands(ctx context.Context) {
	pollRetryDelay := commandRetryDelay
	for ctx.Err() == nil {
		command, ok, err := r.nextCommand(ctx)
		if err != nil {
			if ctx.Err() == nil {
				fmt.Printf("GateLens agent command poll failed: %v\n", err)
				if !waitForRetry(ctx, pollRetryDelay) {
					return
				}
				pollRetryDelay *= 2
				if pollRetryDelay > maxCommandPollRetryDelay {
					pollRetryDelay = maxCommandPollRetryDelay
				}
			}
			continue
		}
		pollRetryDelay = commandRetryDelay
		if !ok {
			continue
		}

		result := r.executeCommand(ctx, command)
		for attempt := 1; attempt <= 3; attempt++ {
			err = r.sendCommandResult(ctx, result)
			if err == nil {
				break
			}
			if attempt == 3 {
				fmt.Printf("GateLens agent command result upload failed: %v\n", err)
				break
			}
			if !waitForRetry(ctx, commandRetryDelay) {
				return
			}
		}
	}
}

func (r *Runner) nextCommand(ctx context.Context) (domain.AgentCommand, bool, error) {
	endpoint := r.config.ServerURL + "/api/v1/agent/commands/next?clusterID=" + url.QueryEscape(r.config.ClusterID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return domain.AgentCommand{}, false, fmt.Errorf("create command poll request: %w", err)
	}
	if r.config.Token != "" {
		request.Header.Set("Authorization", "Bearer "+r.config.Token)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return domain.AgentCommand{}, false, fmt.Errorf("poll commands: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return domain.AgentCommand{}, false, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return domain.AgentCommand{}, false, fmt.Errorf("command server returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}

	var command domain.AgentCommand
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&command); err != nil {
		return domain.AgentCommand{}, false, fmt.Errorf("decode agent command: %w", err)
	}
	if command.ID == "" || command.ClusterID != r.config.ClusterID {
		return domain.AgentCommand{}, false, fmt.Errorf("invalid command identity")
	}
	return command, true, nil
}

func (r *Runner) executeCommand(ctx context.Context, command domain.AgentCommand) domain.AgentCommandResult {
	result := domain.AgentCommandResult{CommandID: command.ID, ClusterID: r.config.ClusterID}
	commandCtx := ctx
	cancel := func() {}
	if deadline, err := time.Parse(time.RFC3339Nano, command.Deadline); err == nil {
		if time.Now().After(deadline) {
			result.Error = "command deadline exceeded before execution"
			return result
		}
		commandCtx, cancel = context.WithDeadline(ctx, deadline)
	}
	defer cancel()

	switch command.Kind {
	case domain.AgentCommandEnvoyConfig:
		config, err := r.reader.EnvoyConfig(commandCtx, command.GatewayID)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Config = &config
	default:
		result.Error = fmt.Sprintf("unsupported agent command kind %q", command.Kind)
	}
	return result
}

func (r *Runner) sendCommandResult(ctx context.Context, result domain.AgentCommandResult) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode command result: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.config.ServerURL+"/api/v1/agent/command-results", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create command result request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if r.config.Token != "" {
		request.Header.Set("Authorization", "Bearer "+r.config.Token)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("upload command result: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("command result server returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	return nil
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
