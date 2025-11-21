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
	
	// 将白名单节点添加为受信任节点并主动连接
	for _, node := range nodes {
		// 先添加为受信任节点（这样可以在MaxPeers限制之外连接）
		w.server.AddTrustedPeer(node)
		
		// 主动发起连接（作为静态节点添加，会自动重连）
		w.server.AddPeer(node)
		
		log.Info("Added and dialing whitelisted node", 
			"id", node.ID().String()[:16], 
			"ip", node.IPAddr(),
			"tcp", node.TCP())
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

