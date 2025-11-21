package eth

import (
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// WhitelistNodesConnector 负责在节点启动时连接白名单中的节点
type WhitelistNodesConnector struct {
	handler *handler
	server  *p2p.Server
}

// NewWhitelistNodesConnector 创建白名单节点连接器
func NewWhitelistNodesConnector(h *handler, srv *p2p.Server) *WhitelistNodesConnector {
	return &WhitelistNodesConnector{
		handler: h,
		server:  srv,
	}
}

// ConnectWhitelistNodes 连接白名单中的节点
func (w *WhitelistNodesConnector) ConnectWhitelistNodes() {
	if w.handler == nil || w.handler.peerWhitelist == nil || w.server == nil {
		return
	}
	
	// 获取白名单节点
	nodes := w.handler.peerWhitelist.getWhitelistNodes()
	if len(nodes) == 0 {
		log.Info("No whitelist nodes to connect")
		return
	}
	
	log.Info("Attempting to connect to whitelisted low-latency nodes", "count", len(nodes))
	
	// 将白名单节点添加为受信任节点，以便优先连接
	// 注意：我们不想永久添加它们，只是在启动时优先尝试连接
	for _, node := range nodes {
		// 使用AddTrustedPeer临时添加为受信任节点
		// 这样它们可以在MaxPeers限制之外连接
		w.server.AddTrustedPeer(node)
		log.Debug("Added whitelisted node as trusted", "id", node.ID().String()[:8], "ip", node.IPAddr())
	}
}

// GetWhitelistNodes 返回白名单节点列表（供外部查询使用）
func (h *handler) GetWhitelistNodes() []*enode.Node {
	if h.peerWhitelist == nil {
		return nil
	}
	return h.peerWhitelist.getWhitelistNodes()
}

// GetWhitelistEntries 返回白名单条目信息（供调试使用）
func (h *handler) GetWhitelistEntries() map[string]whitelistEntry {
	if h.peerWhitelist == nil {
		return nil
	}
	return h.peerWhitelist.list()
}

