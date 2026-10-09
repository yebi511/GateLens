package kube

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gatelens/gatelens/internal/domain"
)

func probeLogLine(route, details string, code int) string {
	return fmt.Sprintf(`{"gatelens_probe_id":"probe","start_time":"2026-09-17T07:34:01.306Z","downstream_remote_address":"10.0.0.1:1234","downstream_local_address":"10.0.0.2:80","route_name":%q,"response_code":%d,"response_code_details":%q}`+"\n", route, code, details)
}

func runtimeSnapshot(content string) probeLogSnapshot {
	return probeLogSnapshot{source: "edge/ns/pod/uid/proxy", runtime: "edge/ns/pod/uid/proxy", pod: "ns/pod", content: content}
}

func TestProbeCachePreservesOccurrencesAndSourceOrder(t *testing.T) {
	first := probeLogLine("first", "internal_redirect", 400)
	last := probeLogLine("last", "via_upstream", 200)
	cache := &probeLogCache{}
	cache.merge(runtimeSnapshot(first), "probe", "", "edge")
	id := cache.hops[0].ID
	cache.merge(runtimeSnapshot(first+last+last), "probe", "", "edge")
	cache.merge(runtimeSnapshot(first+last+last), "probe", "", "edge")
	if len(cache.hops) != 3 || cache.hops[0].ID != id || cache.hops[1].ID == cache.hops[2].ID {
		t.Fatalf("hops=%+v", cache.hops)
	}
	for i, hop := range cache.hops {
		if hop.LogSequence != i+1 || hop.RuntimeSource == "" || hop.RequestStartTime != cache.hops[0].RequestStartTime || strings.Contains(hop.ID, "first") {
			t.Fatalf("hop=%+v", hop)
		}
	}
	other := runtimeSnapshot(last)
	other.source, other.runtime, other.pod = "other", "other", "ns/other"
	cache.merge(other, "probe", "", "edge")
	if cache.hops[3].LogSequence != 1 || cache.hops[3].RuntimeSource == cache.hops[0].RuntimeSource {
		t.Fatal("mixed Pod source order")
	}
}

func TestProbeCacheMarksSnapshotTruncation(t *testing.T) {
	first, last := probeLogLine("first", "internal_redirect", 400), probeLogLine("last", "via_upstream", 200)
	cache := &probeLogCache{}
	cache.merge(runtimeSnapshot(first+last), "probe", "", "edge")
	cache.merge(runtimeSnapshot(last), "probe", "", "edge")
	if len(cache.hops) != 2 || len(cache.issues) == 0 || cache.canSettle() {
		t.Fatalf("cache=%+v", cache)
	}
}

func TestProbeCacheMarksFileRotationAndKeepsRecords(t *testing.T) {
	dir := t.TempDir()
	firstPath, secondPath := filepath.Join(dir, "first.log"), filepath.Join(dir, "second.log")
	first, last := probeLogLine("first", "internal_redirect", 400), probeLogLine("last", "via_upstream", 200)
	if err := os.WriteFile(firstPath, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte(last), 0600); err != nil {
		t.Fatal(err)
	}
	firstInfo, _ := os.Stat(firstPath)
	secondInfo, _ := os.Stat(secondPath)
	cache := &probeLogCache{}
	cache.merge(probeLogSnapshot{source: "file", content: first, fileInfo: firstInfo}, "probe", "", "edge")
	cache.merge(probeLogSnapshot{source: "file", content: last, fileInfo: secondInfo}, "probe", "", "edge")
	if len(cache.hops) != 2 || len(cache.issues) == 0 || cache.hops[0].ID == cache.hops[1].ID {
		t.Fatalf("cache=%+v", cache)
	}
}

func TestProbeWindowWaitsForDelayedTerminal(t *testing.T) {
	first, last := probeLogLine("first", "internal_redirect:custom", 400), probeLogLine("last", "via_upstream", 200)
	started := time.Now()
	reads := 0
	read := func(context.Context) ([]probeLogSnapshot, error) {
		reads++
		content := first
		if time.Since(started) >= 20*time.Millisecond {
			content += last
		}
		return []probeLogSnapshot{runtimeSnapshot(content)}, nil
	}
	logs := collectProbeWindow(context.Background(), read, "probe", "", "edge", time.Second, 5*time.Millisecond, 15*time.Millisecond)
	hops, state := logs.hops, logs.collection
	if len(hops) != 2 || state.State != domain.ProbeCollectionStateSettled || reads < 3 {
		t.Fatalf("hops=%+v state=%+v reads=%d", hops, state, reads)
	}
}

func TestProbeWindowBoundedAndPreservesPartialEvidence(t *testing.T) {
	for _, mode := range []string{"only-redirect", "read-error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reads := 0
			read := func(context.Context) ([]probeLogSnapshot, error) {
				reads++
				if reads > 1 && mode == "read-error" {
					return nil, fmt.Errorf("read failed")
				}
				if reads > 1 && mode == "cancelled" {
					cancel()
					return nil, ctx.Err()
				}
				return []probeLogSnapshot{runtimeSnapshot(probeLogLine("first", "internal_redirect", 400))}, nil
			}
			started := time.Now()
			logs := collectProbeWindow(ctx, read, "probe", "", "edge", 30*time.Millisecond, 5*time.Millisecond, 10*time.Millisecond)
			hops, state := logs.hops, logs.collection
			want := domain.ProbeCollectionStateWindowEnded
			switch mode {
			case "read-error":
				want = domain.ProbeCollectionStateReadError
			case "cancelled":
				want = domain.ProbeCollectionStateCancelled
			}
			if len(hops) != 1 || state.State != want || len(logs.issues) == 0 || time.Since(started) > time.Second {
				t.Fatalf("hops=%+v state=%+v", hops, state)
			}
		})
	}
}

func TestProbeReadUsesRemainingWindowDeadline(t *testing.T) {
	read := func(ctx context.Context) ([]probeLogSnapshot, error) { <-ctx.Done(); return nil, ctx.Err() }
	started := time.Now()
	logs := collectProbeWindow(context.Background(), read, "probe", "", "edge", 20*time.Millisecond, time.Millisecond, time.Millisecond)
	state := logs.collection
	if state.State != domain.ProbeCollectionStateWindowEnded || time.Since(started) > time.Second {
		t.Fatalf("state=%+v", state)
	}
}

func TestExpiredReadCannotUpgradeEvidenceToSettled(t *testing.T) {
	reads := 0
	read := func(ctx context.Context) ([]probeLogSnapshot, error) {
		reads++
		if reads > 1 {
			<-ctx.Done()
		}
		return []probeLogSnapshot{runtimeSnapshot(probeLogLine("terminal", "via_upstream", 200))}, nil
	}
	logs := collectProbeWindow(context.Background(), read, "probe", "", "edge", 20*time.Millisecond, time.Millisecond, time.Millisecond)
	hops, state := logs.hops, logs.collection
	if len(hops) != 1 || state.State != domain.ProbeCollectionStateWindowEnded {
		t.Fatalf("hops=%+v state=%+v", hops, state)
	}
}

func TestProbeCollectionIssuesIgnoreDisplayWording(t *testing.T) {
	cache := &probeLogCache{}
	cache.merge(runtimeSnapshot(probeLogLine("terminal", "via_upstream", 200)), "probe", "", "edge")
	if !cache.canSettle() {
		t.Fatal("valid terminal did not settle")
	}
	cache.addIssue(domain.ProbeIssueCodeLogOrderUnconfirmed, "wording with no order keywords")
	if cache.canSettle() {
		t.Fatal("structured issue did not block stability")
	}
	cache.issues[0].Message = "another language"
	if cache.canSettle() {
		t.Fatal("message affected stability")
	}
	snapshot := runtimeSnapshot(probeLogLine("second", "via_upstream", 200))
	snapshot.incomplete = true
	cache.merge(snapshot, "probe", "", "edge")
	found := false
	for _, issue := range cache.issues {
		if issue.Code == domain.ProbeIssueCodeLogWindowTruncated {
			found = true
		}
	}
	if !found || len(cache.hops) < 1 {
		t.Fatalf("cache=%+v", cache)
	}
}
