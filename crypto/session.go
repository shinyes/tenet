package crypto

import (
	"sync"
)

// SessionManager 会话管理器
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewSessionManager 创建会话管理器
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// Add 添加会话
func (sm *SessionManager) Add(peerID string, session *Session) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[peerID] = session
}

// Get 获取会话
func (sm *SessionManager) Get(peerID string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[peerID]
}

// Remove 移除会话
func (sm *SessionManager) Remove(peerID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, peerID)
}

// Has 检查会话是否存在
func (sm *SessionManager) Has(peerID string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	_, ok := sm.sessions[peerID]
	return ok
}

// Count 获取会话数量
func (sm *SessionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// All 获取所有会话
func (sm *SessionManager) All() map[string]*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]*Session, len(sm.sessions))
	for k, v := range sm.sessions {
		result[k] = v
	}
	return result
}

// Clear 清空所有会话，安全清除密钥材料
func (sm *SessionManager) Clear() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	// 安全关闭所有会话，清除密钥材料
	for _, session := range sm.sessions {
		if session != nil {
			session.Close()
		}
	}
	sm.sessions = make(map[string]*Session)
}
