package eth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

const (
	// 延迟低于此阈值的节点将被加入白名单 (30ms)
	DefaultLatencyThreshold = 30 // milliseconds
	// 白名单最大保存节点数量
	DefaultMaxWhitelistSize = 100
	// 节点需要至少连接多久才能被考虑加入白名单（避免短暂连接）
	MinConnectionDuration = 5 * time.Minute
)

type peerWhitelistConfig struct {
	Enabled           bool
	LatencyThreshold  int64  // 延迟阈值（毫秒）
	MaxSize           int    // 白名单最大节点数
	Path              string // 持久化文件路径
	MinConnDuration   time.Duration // 最小连接时长
}

type whitelistEntry struct {
	Enode            string    `json:"enode"`            // 节点的enode URL
	LastSeen         time.Time `json:"lastSeen"`         // 最后连接时间
	AverageLatency   int64     `json:"averageLatency"`   // 平均延迟（毫秒）
	ConnectionCount  uint64    `json:"connectionCount"`  // 连接次数
	TotalConnTime    uint64    `json:"totalConnTime"`    // 总连接时间（秒）
	LastConnDuration uint64    `json:"lastConnDuration"` // 最后一次连接时长（秒）
}

type whitelistFile struct {
	Entries []whitelistEntry `json:"entries"`
}

// 运行时节点统计信息
type peerLatencyStats struct {
	ConnectedAt    time.Time
	LatencySamples []int64 // 记录所有延迟采样
	EnodeURL       string
}

type peerWhitelist struct {
	cfg           peerWhitelistConfig
	mu            sync.RWMutex
	entries       map[string]*whitelistEntry // key: node ID
	runtimeStats  map[string]*peerLatencyStats // key: node ID，运行时统计
}

func newPeerWhitelist(cfg peerWhitelistConfig) (*peerWhitelist, error) {
	if cfg.Path == "" {
		return nil, nil
	}
	
	// 设置默认值
	if cfg.LatencyThreshold == 0 {
		cfg.LatencyThreshold = DefaultLatencyThreshold
	}
	if cfg.MaxSize == 0 {
		cfg.MaxSize = DefaultMaxWhitelistSize
	}
	if cfg.MinConnDuration == 0 {
		cfg.MinConnDuration = MinConnectionDuration
	}

	wl := &peerWhitelist{
		cfg:          cfg,
		entries:      make(map[string]*whitelistEntry),
		runtimeStats: make(map[string]*peerLatencyStats),
	}
	
	if err := wl.load(); err != nil {
		log.Warn("Failed to load peer whitelist, starting with empty whitelist", "err", err)
		// 不返回错误，继续使用空白名单
	}
	
	return wl, nil
}

func (wl *peerWhitelist) load() error {
	wl.mu.Lock()
	defer wl.mu.Unlock()

	data, err := os.ReadFile(wl.cfg.Path)
	if errors.Is(err, os.ErrNotExist) {
		log.Info("Peer whitelist file does not exist, will create on first save", "path", wl.cfg.Path)
		return nil
	}
	if err != nil {
		return err
	}
	
	var file whitelistFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	
	// 加载白名单条目
	for _, entry := range file.Entries {
		// 尝试解析enode URL获取node ID
		node, err := enode.Parse(enode.ValidSchemes, entry.Enode)
		if err != nil {
			log.Warn("Failed to parse enode in whitelist, skipping", "enode", entry.Enode, "err", err)
			continue
		}
		
		// 复制entry以避免指针问题
		entryCopy := entry
		wl.entries[node.ID().String()] = &entryCopy
	}
	
	log.Info("Loaded peer whitelist", "count", len(wl.entries), "path", wl.cfg.Path)
	return nil
}

func (wl *peerWhitelist) persistLocked() {
	if wl.cfg.Path == "" {
		return
	}
	
	// 创建目录
	if err := os.MkdirAll(filepath.Dir(wl.cfg.Path), 0o755); err != nil {
		log.Warn("Failed to create peer whitelist directory", "path", wl.cfg.Path, "err", err)
		return
	}
	
	// 转换为数组并按延迟排序
	entries := make([]whitelistEntry, 0, len(wl.entries))
	for _, entry := range wl.entries {
		entries = append(entries, *entry)
	}
	
	// 如果超过最大数量，只保留延迟最低的
	if len(entries) > wl.cfg.MaxSize {
		// 简单排序：按延迟从低到高
		for i := 0; i < len(entries)-1; i++ {
			for j := i + 1; j < len(entries); j++ {
				if entries[j].AverageLatency < entries[i].AverageLatency {
					entries[i], entries[j] = entries[j], entries[i]
				}
			}
		}
		entries = entries[:wl.cfg.MaxSize]
	}
	
	payload := whitelistFile{Entries: entries}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		log.Warn("Failed to marshal peer whitelist", "err", err)
		return
	}
	
	if err := os.WriteFile(wl.cfg.Path, data, 0o644); err != nil {
		log.Warn("Failed to persist peer whitelist", "path", wl.cfg.Path, "err", err)
		return
	}
	
	log.Debug("Peer whitelist persisted", "count", len(entries), "path", wl.cfg.Path)
}

// 节点连接时调用
func (wl *peerWhitelist) onPeerConnected(id string, enodeURL string) {
	if wl == nil || !wl.cfg.Enabled {
		return
	}
	
	wl.mu.Lock()
	defer wl.mu.Unlock()
	
	wl.runtimeStats[id] = &peerLatencyStats{
		ConnectedAt:    time.Now(),
		LatencySamples: make([]int64, 0, 100),
		EnodeURL:       enodeURL,
	}
}

// 记录延迟样本
func (wl *peerWhitelist) recordLatency(id string, latency int64) {
	if wl == nil || !wl.cfg.Enabled {
		return
	}
	
	wl.mu.Lock()
	defer wl.mu.Unlock()
	
	stats, exists := wl.runtimeStats[id]
	if !exists {
		return
	}
	
	// 记录延迟采样
	stats.LatencySamples = append(stats.LatencySamples, latency)
}

// 节点断开连接时调用
func (wl *peerWhitelist) onPeerDisconnected(id string) {
	if wl == nil || !wl.cfg.Enabled {
		return
	}
	
	wl.mu.Lock()
	defer wl.mu.Unlock()
	
	stats, exists := wl.runtimeStats[id]
	if !exists {
		return
	}
	
	// 计算连接时长
	connDuration := time.Since(stats.ConnectedAt)
	
	// 检查是否满足最小连接时长要求
	if connDuration < wl.cfg.MinConnDuration {
		log.Debug("Peer connection too short to consider for whitelist", 
			"id", id, "duration", connDuration)
		delete(wl.runtimeStats, id)
		return
	}
	
	// 计算平均延迟
	if len(stats.LatencySamples) == 0 {
		log.Debug("No latency samples for peer", "id", id)
		delete(wl.runtimeStats, id)
		return
	}
	
	var sum int64
	for _, lat := range stats.LatencySamples {
		sum += lat
	}
	avgLatency := sum / int64(len(stats.LatencySamples))
	
	// 检查是否满足延迟阈值
	if avgLatency > wl.cfg.LatencyThreshold {
		log.Debug("Peer latency too high for whitelist", 
			"id", id, "avgLatency", avgLatency, "threshold", wl.cfg.LatencyThreshold)
		delete(wl.runtimeStats, id)
		return
	}
	
	// 更新或创建白名单条目
	entry, exists := wl.entries[id]
	if !exists {
		entry = &whitelistEntry{
			Enode: stats.EnodeURL,
		}
		wl.entries[id] = entry
	}
	
	entry.LastSeen = time.Now()
	entry.AverageLatency = avgLatency
	entry.ConnectionCount++
	entry.LastConnDuration = uint64(connDuration.Seconds())
	entry.TotalConnTime += uint64(connDuration.Seconds())
	
	log.Info("Peer added/updated in whitelist", 
		"id", id, 
		"avgLatency", avgLatency, 
		"samples", len(stats.LatencySamples),
		"duration", connDuration,
		"connectionCount", entry.ConnectionCount)
	
	// 清理运行时统计
	delete(wl.runtimeStats, id)
	
	// 持久化
	wl.persistLocked()
}

// 检查节点是否在白名单中
func (wl *peerWhitelist) isWhitelisted(id string) bool {
	if wl == nil {
		return false
	}
	
	wl.mu.RLock()
	defer wl.mu.RUnlock()
	
	_, ok := wl.entries[id]
	return ok
}

// 获取白名单节点列表（用于启动时优先连接）
func (wl *peerWhitelist) getWhitelistNodes() []*enode.Node {
	if wl == nil {
		return nil
	}
	
	wl.mu.RLock()
	defer wl.mu.RUnlock()
	
	nodes := make([]*enode.Node, 0, len(wl.entries))
	for _, entry := range wl.entries {
		node, err := enode.Parse(enode.ValidSchemes, entry.Enode)
		if err != nil {
			log.Warn("Failed to parse whitelisted enode", "enode", entry.Enode, "err", err)
			continue
		}
		nodes = append(nodes, node)
	}
	
	return nodes
}

// 获取白名单条目列表（用于调试/监控）
func (wl *peerWhitelist) list() map[string]whitelistEntry {
	if wl == nil {
		return nil
	}
	
	wl.mu.RLock()
	defer wl.mu.RUnlock()
	
	result := make(map[string]whitelistEntry, len(wl.entries))
	for id, entry := range wl.entries {
		result[id] = *entry
	}
	return result
}

// 手动添加节点到白名单（用于测试或管理）
func (wl *peerWhitelist) addManually(enodeURL string, avgLatency int64) error {
	if wl == nil || !wl.cfg.Enabled {
		return errors.New("whitelist not enabled")
	}
	
	node, err := enode.Parse(enode.ValidSchemes, enodeURL)
	if err != nil {
		return err
	}
	
	wl.mu.Lock()
	defer wl.mu.Unlock()
	
	id := node.ID().String()
	entry := &whitelistEntry{
		Enode:           enodeURL,
		LastSeen:        time.Now(),
		AverageLatency:  avgLatency,
		ConnectionCount: 1,
	}
	
	wl.entries[id] = entry
	wl.persistLocked()
	
	log.Info("Manually added peer to whitelist", "id", id, "enode", enodeURL, "latency", avgLatency)
	return nil
}

// 从白名单中移除节点
func (wl *peerWhitelist) remove(id string) {
	if wl == nil {
		return
	}
	
	wl.mu.Lock()
	defer wl.mu.Unlock()
	
	if _, exists := wl.entries[id]; exists {
		delete(wl.entries, id)
		wl.persistLocked()
		log.Info("Removed peer from whitelist", "id", id)
	}
}

