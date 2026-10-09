package kube

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gatelens/gatelens/internal/domain"
	"github.com/gatelens/gatelens/internal/observed"
)

type probeLogSnapshot struct {
	source, runtime, pod, content string
	fileInfo                      os.FileInfo
	offset                        int64
	incomplete                    bool
}

type probeLogSourceCache struct {
	seen       map[string]int
	previous   []string
	fileInfo   os.FileInfo
	generation int
	sequence   int
	uncertain  bool
}

type probeLogCache struct {
	sources map[string]*probeLogSourceCache
	hops    []domain.ObservedHop
	issues  []domain.ProbeIssue
}

func (cache *probeLogCache) addIssue(code domain.ProbeIssueCode, message string) {
	cache.issues = domain.AppendProbeIssue(cache.issues, domain.ProbeIssue{Scope: domain.ProbeIssueScopeCollection, Code: code, Message: message})
}

// probeLogResult carries one gateway's bounded collection without duplicating reasons.
type probeLogResult struct {
	hops       []domain.ObservedHop
	collection *domain.ProbeCollection
	issues     []domain.ProbeIssue
}

// merge preserves occurrences, rather than deduplicating by timestamp or Route.
func (cache *probeLogCache) merge(snapshot probeLogSnapshot, probeID, traceID, clusterID string) bool {
	if cache.sources == nil {
		cache.sources = map[string]*probeLogSourceCache{}
	}
	source := cache.sources[snapshot.source]
	if source == nil {
		source = &probeLogSourceCache{seen: map[string]int{}}
		cache.sources[snapshot.source] = source
	}
	if snapshot.fileInfo != nil && source.fileInfo != nil && (!os.SameFile(snapshot.fileInfo, source.fileInfo) || snapshot.fileInfo.Size() < source.fileInfo.Size()) {
		source.generation++
		source.uncertain = true
		cache.addIssue(domain.ProbeIssueCodeLogSourceRotated, "访问日志文件发生轮转或截断，来源连续性未确认")
	}
	source.fileInfo = snapshot.fileInfo
	if snapshot.incomplete {
		source.uncertain = true
		cache.addIssue(domain.ProbeIssueCodeLogWindowTruncated, "日志读取窗口被截断，来源连续性未确认")
	}
	occurrences := map[[32]byte]int{}
	var current []string
	changed := false
	position := snapshot.offset
	for _, line := range strings.Split(snapshot.content, "\n") {
		hop, matched, _ := observed.ParseHigressLineForProbe(line, probeID, traceID, clusterID, snapshot.pod, snapshot.source)
		linePosition := position
		position += int64(len(line) + 1)
		if !matched {
			continue
		}
		// CRI prefixes do not identify the JSON access-log occurrence.
		identityLine := strings.TrimSpace(line)
		if index := strings.IndexByte(identityLine, '{'); index > 0 {
			identityLine = identityLine[index:]
		}
		fingerprint := sha256.Sum256([]byte(identityLine))
		occurrences[fingerprint]++
		key := fmt.Sprintf("%x/%d", fingerprint, occurrences[fingerprint])
		if snapshot.fileInfo != nil {
			key = fmt.Sprintf("%d/%d/%x", source.generation, linePosition, fingerprint)
		}
		current = append(current, key)
		if _, exists := source.seen[key]; exists {
			continue
		}
		source.sequence++
		hop.ID = fmt.Sprintf("record-%d", len(cache.hops)+1)
		hop.LogSourceID = snapshot.source
		hop.RuntimeSource = snapshot.runtime
		hop.LogSequence = source.sequence
		source.seen[key] = len(cache.hops)
		cache.hops = append(cache.hops, hop)
		changed = true
	}
	// Every previously observed occurrence must still be a prefix of a full
	// snapshot. Otherwise the source cannot establish a continuous order.
	if len(source.previous) > 0 {
		prefix := len(current) >= len(source.previous)
		if prefix {
			for i, key := range source.previous {
				if current[i] != key {
					prefix = false
					break
				}
			}
		}
		if !prefix {
			source.uncertain = true
			cache.addIssue(domain.ProbeIssueCodeLogOrderUnconfirmed, "日志快照发生截断或重叠无法定位，来源顺序未确认")
		}
	}
	source.previous = current
	return changed
}

func (cache *probeLogCache) canSettle() bool {
	if len(cache.hops) == 0 {
		return false
	}
	for _, issue := range cache.issues {
		if issue.BreaksLogOrder() {
			return false
		}
	}
	groups := map[string][]domain.ObservedHop{}
	for _, hop := range cache.hops {
		if hop.RuntimeSource == "" || hop.RequestStartTime == "" || !usableAddress(hop.DownstreamRemoteAddress) {
			return false
		}
		key := hop.RuntimeSource + "\x00" + hop.RequestStartTime + "\x00" + hop.DownstreamRemoteAddress + "\x00" + hop.DownstreamLocalAddress
		groups[key] = append(groups[key], hop)
	}
	for _, hops := range groups {
		terminal := 0
		for _, hop := range hops {
			if !hop.InternalRedirect {
				terminal++
			}
		}
		if terminal != 1 || hops[len(hops)-1].InternalRedirect {
			return false
		}
	}
	return true
}

func usableAddress(value string) bool {
	return strings.TrimSpace(value) != "" && strings.TrimSpace(value) != "-"
}

type probeLogReader func(context.Context) ([]probeLogSnapshot, error)

func collectProbeWindow(ctx context.Context, read probeLogReader, probeID, traceID, clusterID string, window, interval, settle time.Duration) probeLogResult {
	windowCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	cache := &probeLogCache{hops: []domain.ObservedHop{}, issues: []domain.ProbeIssue{}}
	collection := &domain.ProbeCollection{State: domain.ProbeCollectionStateWindowEnded}
	var stableSince time.Time
	finish := func(state domain.ProbeCollectionState, code domain.ProbeIssueCode, reason string) probeLogResult {
		collection.State, collection.CompletedAt = state, time.Now().UTC().Format(time.RFC3339Nano)
		if reason != "" {
			cache.addIssue(code, reason)
		}
		return probeLogResult{hops: cache.hops, collection: collection, issues: cache.issues}
	}
	for {
		if windowCtx.Err() != nil {
			if ctx.Err() != nil {
				return finish(domain.ProbeCollectionStateCancelled, domain.ProbeIssueCodeCollectionCancelled, "日志采集已取消："+ctx.Err().Error())
			}
			return finish(domain.ProbeCollectionStateWindowEnded, domain.ProbeIssueCodeCollectionWindowEnded, "日志采集窗口已结束，未确认稳定终止记录")
		}
		snapshots, err := read(windowCtx)
		changed := false
		for _, snapshot := range snapshots {
			if cache.merge(snapshot, probeID, traceID, clusterID) {
				changed = true
			}
		}
		// Preserve a final partial read, but never call it settled after its
		// observation deadline (even if a reader returns a nil error).
		if windowCtx.Err() != nil {
			if ctx.Err() != nil {
				return finish(domain.ProbeCollectionStateCancelled, domain.ProbeIssueCodeCollectionCancelled, "日志采集已取消："+ctx.Err().Error())
			}
			return finish(domain.ProbeCollectionStateWindowEnded, domain.ProbeIssueCodeCollectionWindowEnded, "日志采集窗口已结束，读取受剩余时间限制")
		}
		if err != nil {
			if ctx.Err() != nil {
				return finish(domain.ProbeCollectionStateCancelled, domain.ProbeIssueCodeCollectionCancelled, "日志采集已取消："+ctx.Err().Error())
			}
			return finish(domain.ProbeCollectionStateReadError, domain.ProbeIssueCodeLogReadError, err.Error())
		}
		if changed || !cache.canSettle() {
			stableSince = time.Time{}
		}
		if cache.canSettle() {
			if stableSince.IsZero() {
				stableSince = time.Now()
			} else if time.Since(stableSince) >= settle {
				return finish(domain.ProbeCollectionStateSettled, "", "")
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-windowCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (s *Store) observeProbeWindow(ctx context.Context, runtime gatewayRuntime, since time.Time, fileOffset int64, probeID, traceID string, window time.Duration) probeLogResult {
	logSource := "kubernetes-pod-log"
	if s.probeLogFile != "" {
		logSource = "file:" + s.probeLogFile
	}
	var previousFile os.FileInfo
	read := func(readCtx context.Context) ([]probeLogSnapshot, error) {
		if s.probeLogFile != "" {
			file, err := os.Open(s.probeLogFile)
			if err != nil {
				return nil, fmt.Errorf("读取访问日志文件 %q: %w", s.probeLogFile, err)
			}
			defer file.Close()
			info, err := file.Stat()
			if err != nil {
				return nil, err
			}
			if info.Size() < fileOffset || (previousFile != nil && (!os.SameFile(info, previousFile) || info.Size() < previousFile.Size())) {
				fileOffset = 0
			}
			previousFile = info
			offset := fileOffset
			incomplete := false
			if info.Size()-offset > maxProbeLogBytes {
				offset = info.Size() - maxProbeLogBytes
				incomplete = true
			}
			if _, err := file.Seek(offset, io.SeekStart); err != nil {
				return nil, err
			}
			content, err := io.ReadAll(io.LimitReader(file, maxProbeLogBytes+1))
			if err != nil {
				return nil, err
			}
			if len(content) > maxProbeLogBytes {
				content = content[:maxProbeLogBytes]
				incomplete = true
			}
			// A mounted file alone cannot prove which Pod/Container emitted it.
			return []probeLogSnapshot{{source: logSource, content: string(content), fileInfo: info, offset: offset, incomplete: incomplete}}, nil
		}
		var snapshots []probeLogSnapshot
		var readErrors []string
		for _, pod := range runtime.Pods {
			options := &corev1.PodLogOptions{Container: pod.Container, SinceTime: &metav1.Time{Time: since}, Timestamps: false}
			stream, err := s.core.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, options).Stream(readCtx)
			if err != nil {
				readErrors = append(readErrors, fmt.Sprintf("%s/%s: %v", pod.Namespace, pod.Name, err))
				continue
			}
			content, err := io.ReadAll(io.LimitReader(stream, maxProbeLogBytes+1))
			_ = stream.Close()
			incomplete := len(content) > maxProbeLogBytes
			if incomplete {
				content = content[:maxProbeLogBytes]
			}
			identity := s.clusterID + "/" + pod.Namespace + "/" + pod.Name + "/" + string(pod.UID) + "/" + pod.Container
			snapshots = append(snapshots, probeLogSnapshot{
				source:     identity,
				runtime:    identity,
				pod:        pod.Namespace + "/" + pod.Name,
				content:    string(content),
				incomplete: incomplete})
			if err != nil {
				readErrors = append(readErrors, fmt.Sprintf("%s/%s: %v", pod.Namespace, pod.Name, err))
			}
		}
		if len(readErrors) > 0 {
			return snapshots, fmt.Errorf("读取 Gateway Pod 日志失败: %s", strings.Join(readErrors, "; "))
		}
		return snapshots, nil
	}
	return collectProbeWindow(ctx, read, probeID, traceID, s.clusterID, window, 200*time.Millisecond, 500*time.Millisecond)
}
