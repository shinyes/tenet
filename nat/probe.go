package nat

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

// NAT 探测协议常量
const (
	// 探测包类型
	ProbeTypeRequest  = 0x01 // 请求探测
	ProbeTypeResponse = 0x02 // 响应探测
	ProbeTypeResult   = 0x03 // 探测结果

	// 探测魔术数
	ProbeMagic = "NATP"
)

// ProbeResult NAT 探测结果
type ProbeResult struct {
	// 检测到的 NAT 类型
	NATType NATType
	// 检测到的公网地址
	PublicAddr *net.UDPAddr
	// 是否支持端口预测（对称NAT时有用）
	PortPredictable bool
	// 端口增量（如果可预测）
	PortDelta int
	// 探测时间
	ProbeTime time.Time
	// 探测节点ID
	ProberID string
}

// ProbeRequest NAT 探测请求
type ProbeRequest struct {
	// 请求ID
	RequestID uint32
	// 请求者期望的探测端口数量
	PortCount int
}

// ProbeResponse NAT 探测响应
type ProbeResponse struct {
	// 请求ID
	RequestID uint32
	// 探测者看到的请求者地址
	ObservedAddr *net.UDPAddr
	// 从哪个端口发送的响应
	FromPort int
}

// NATProber 节点辅助 NAT 探测器
type NATProber struct {
	mu sync.RWMutex

	// 本地 UDP 连接（用于发送探测包）
	conn *net.UDPConn

	// 已知的探测结果缓存
	cachedResult *ProbeResult

	// 待处理的探测请求
	pendingProbes map[uint32]*pendingProbe

	// 作为探测服务器时收到的请求
	probeRequests map[uint32]*probeRequestState

	// 探测超时
	timeout time.Duration

	// 请求ID计数器
	nextRequestID uint32
}

// pendingProbe 待处理的探测
type pendingProbe struct {
	requestID    uint32
	responses    []*ProbeResponse
	responseCh   chan struct{}
	createTime   time.Time
	expectedPort int // 期望的响应端口数量
}

// probeRequestState 探测请求状态（服务端）
type probeRequestState struct {
	requestID   uint32
	fromAddr    *net.UDPAddr
	receivedAt  time.Time
	portCount   int
	respondedAt []time.Time
}

// NewNATProber 创建 NAT 探测器
func NewNATProber(conn *net.UDPConn) *NATProber {
	prober := &NATProber{
		conn:          conn,
		pendingProbes: make(map[uint32]*pendingProbe),
		probeRequests: make(map[uint32]*probeRequestState),
		timeout:       5 * time.Second,
		nextRequestID: 1,
	}

	// 启动定期清理协程，防止内存泄漏
	go prober.cleanupLoop()

	return prober
}

// cleanupLoop 定期清理过期的探测状态
func (p *NATProber) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		p.cleanupStaleProbes()
	}
}

// cleanupStaleProbes 清理过期的探测请求
func (p *NATProber) cleanupStaleProbes() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	staleTimeout := 60 * time.Second

	// 清理过期的待处理探测
	for id, probe := range p.pendingProbes {
		if now.Sub(probe.createTime) > staleTimeout {
			delete(p.pendingProbes, id)
		}
	}

	// 清理过期的探测请求状态
	for id, req := range p.probeRequests {
		if now.Sub(req.receivedAt) > staleTimeout {
			delete(p.probeRequests, id)
		}
	}
}

// ProbeViaHelper 通过辅助节点探测本机 NAT 类型
// helperAddrs: 辅助节点的多个端口地址（同一节点的不同端口）
// 需要至少2个不同端口的辅助地址来判断 NAT 类型
func (p *NATProber) ProbeViaHelper(helperAddrs []*net.UDPAddr) (*ProbeResult, error) {
	if len(helperAddrs) < 1 {
		return nil, fmt.Errorf("需要至少1个辅助节点地址")
	}

	p.mu.Lock()
	requestID := p.nextRequestID
	p.nextRequestID++

	probe := &pendingProbe{
		requestID:    requestID,
		responses:    make([]*ProbeResponse, 0),
		responseCh:   make(chan struct{}, len(helperAddrs)),
		createTime:   time.Now(),
		expectedPort: len(helperAddrs),
	}
	p.pendingProbes[requestID] = probe
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.pendingProbes, requestID)
		p.mu.Unlock()
	}()

	// 向所有辅助地址发送探测请求
	packet := p.buildProbeRequest(requestID, len(helperAddrs))
	for _, addr := range helperAddrs {
		p.conn.WriteToUDP(packet, addr)
	}

	// 等待响应（超时或收到足够响应）
	deadline := time.After(p.timeout)
	received := 0

	for received < len(helperAddrs) {
		select {
		case <-probe.responseCh:
			received++
		case <-deadline:
			goto analyze
		}
	}

analyze:
	p.mu.RLock()
	responses := make([]*ProbeResponse, len(probe.responses))
	copy(responses, probe.responses)
	p.mu.RUnlock()

	return p.analyzeResponses(responses, helperAddrs)
}

// analyzeResponses 分析探测响应，判断 NAT 类型
func (p *NATProber) analyzeResponses(responses []*ProbeResponse, helperAddrs []*net.UDPAddr) (*ProbeResult, error) {
	if len(responses) == 0 {
		return nil, fmt.Errorf("未收到任何探测响应")
	}

	result := &ProbeResult{
		NATType:   NATUnknown,
		ProbeTime: time.Now(),
	}

	// 取第一个响应的观察地址作为公网地址
	result.PublicAddr = responses[0].ObservedAddr

	// 检查是否是公网IP（无NAT）
	if p.conn.LocalAddr() != nil {
		localAddr := p.conn.LocalAddr().(*net.UDPAddr)
		if localAddr.IP.Equal(result.PublicAddr.IP) && localAddr.Port == result.PublicAddr.Port {
			result.NATType = NATNone
			return result, nil
		}
	}

	// 如果只有一个响应，无法确定具体类型
	if len(responses) == 1 {
		result.NATType = NATUnknown
		return result, nil
	}

	// 分析多个响应的端口变化
	ports := make([]int, len(responses))
	for i, resp := range responses {
		ports[i] = resp.ObservedAddr.Port
	}

	// 检查端口是否一致
	allSame := true
	for i := 1; i < len(ports); i++ {
		if ports[i] != ports[0] {
			allSame = false
			break
		}
	}

	if allSame {
		// 端口不变，可能是锥形 NAT
		// 需要进一步测试来区分全锥形、受限锥形、端口受限锥形
		// 但在无外部 STUN 的情况下，我们假设为端口受限锥形（保守估计）
		result.NATType = NATPortRestricted
	} else {
		// 端口变化，是对称型 NAT
		result.NATType = NATSymmetric

		// 检查端口增量是否可预测
		if len(ports) >= 2 {
			delta := ports[1] - ports[0]
			predictable := true
			for i := 2; i < len(ports); i++ {
				if ports[i]-ports[i-1] != delta {
					predictable = false
					break
				}
			}
			result.PortPredictable = predictable
			if predictable {
				result.PortDelta = delta
			}
		}
	}

	// 缓存结果
	p.mu.Lock()
	p.cachedResult = result
	p.mu.Unlock()

	return result, nil
}

// HandleProbePacket 处理收到的探测包（作为探测服务器）
func (p *NATProber) HandleProbePacket(data []byte, from *net.UDPAddr) []byte {
	if len(data) < 8 || string(data[0:4]) != ProbeMagic {
		return nil
	}

	probeType := data[4]

	switch probeType {
	case ProbeTypeRequest:
		return p.handleProbeRequest(data, from)
	case ProbeTypeResponse:
		p.handleProbeResponse(data, from)
		return nil
	default:
		return nil
	}
}

// handleProbeRequest 处理探测请求（作为服务端）
func (p *NATProber) handleProbeRequest(data []byte, from *net.UDPAddr) []byte {
	if len(data) < 9 {
		return nil
	}

	requestID := binary.BigEndian.Uint32(data[5:9])

	// 构造响应：告诉请求者它的公网地址
	return p.buildProbeResponse(requestID, from, p.conn.LocalAddr().(*net.UDPAddr).Port)
}

// handleProbeResponse 处理探测响应（作为客户端）
func (p *NATProber) handleProbeResponse(data []byte, from *net.UDPAddr) {
	if len(data) < 15 {
		return
	}

	requestID := binary.BigEndian.Uint32(data[5:9])
	observedPort := int(binary.BigEndian.Uint16(data[9:11]))
	ipLen := int(data[11])
	if len(data) < 12+ipLen+2 {
		return
	}
	observedIP := net.IP(data[12 : 12+ipLen])
	fromPort := int(binary.BigEndian.Uint16(data[12+ipLen : 14+ipLen]))

	response := &ProbeResponse{
		RequestID:    requestID,
		ObservedAddr: &net.UDPAddr{IP: observedIP, Port: observedPort},
		FromPort:     fromPort,
	}

	p.mu.Lock()
	if probe, exists := p.pendingProbes[requestID]; exists {
		probe.responses = append(probe.responses, response)
		select {
		case probe.responseCh <- struct{}{}:
		default:
		}
	}
	p.mu.Unlock()
}

// buildProbeRequest 构建探测请求包
func (p *NATProber) buildProbeRequest(requestID uint32, portCount int) []byte {
	// 格式: [Magic(4)] [Type(1)] [RequestID(4)] [PortCount(1)]
	packet := make([]byte, 10)
	copy(packet[0:4], ProbeMagic)
	packet[4] = ProbeTypeRequest
	binary.BigEndian.PutUint32(packet[5:9], requestID)
	packet[9] = byte(portCount)
	return packet
}

// buildProbeResponse 构建探测响应包
func (p *NATProber) buildProbeResponse(requestID uint32, observedAddr *net.UDPAddr, fromPort int) []byte {
	// 格式: [Magic(4)] [Type(1)] [RequestID(4)] [ObservedPort(2)] [IPLen(1)] [IP(N)] [FromPort(2)]
	ip := observedAddr.IP.To4()
	if ip == nil {
		ip = observedAddr.IP.To16()
	}
	ipLen := len(ip)

	packet := make([]byte, 14+ipLen)
	copy(packet[0:4], ProbeMagic)
	packet[4] = ProbeTypeResponse
	binary.BigEndian.PutUint32(packet[5:9], requestID)
	binary.BigEndian.PutUint16(packet[9:11], uint16(observedAddr.Port))
	packet[11] = byte(ipLen)
	copy(packet[12:12+ipLen], ip)
	binary.BigEndian.PutUint16(packet[12+ipLen:14+ipLen], uint16(fromPort))

	return packet
}

// GetCachedResult 获取缓存的探测结果
func (p *NATProber) GetCachedResult() *ProbeResult {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cachedResult
}

// ClearCache 清除缓存
func (p *NATProber) ClearCache() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cachedResult = nil
}

// SelectPunchStrategy 根据双方 NAT 类型选择打洞策略
func SelectPunchStrategy(localNAT, remoteNAT NATType) PunchStrategy {
	// 策略优先级矩阵
	switch {
	// 任一方是公网IP，直接连接
	case localNAT == NATNone || remoteNAT == NATNone:
		return StrategyDirect

	// 双方都是锥形 NAT（全锥/受限/端口受限），UDP 打洞成功率高
	case localNAT <= NATPortRestricted && remoteNAT <= NATPortRestricted:
		return StrategyUDPPunch

	// 一方是对称型，另一方是锥形
	case (localNAT == NATSymmetric && remoteNAT <= NATPortRestricted) ||
		(remoteNAT == NATSymmetric && localNAT <= NATPortRestricted):
		return StrategyTCPPunch

	// 双方都是对称型，尝试端口预测
	case localNAT == NATSymmetric && remoteNAT == NATSymmetric:
		return StrategyPortPredict

	// 未知类型，尝试所有方法
	default:
		return StrategyAll
	}
}

// PunchStrategy 打洞策略
type PunchStrategy int

const (
	// StrategyDirect 直接连接（无需打洞）
	StrategyDirect PunchStrategy = iota
	// StrategyUDPPunch UDP 打洞（优先）
	StrategyUDPPunch
	// StrategyTCPPunch TCP 同时打开
	StrategyTCPPunch
	// StrategyPortPredict 端口预测（对称NAT）
	StrategyPortPredict
	// StrategyRelay 中继（回退方案）
	StrategyRelay
	// StrategyAll 尝试所有方法
	StrategyAll
)

func (s PunchStrategy) String() string {
	switch s {
	case StrategyDirect:
		return "Direct"
	case StrategyUDPPunch:
		return "UDP Punch"
	case StrategyTCPPunch:
		return "TCP Punch"
	case StrategyPortPredict:
		return "Port Predict"
	case StrategyRelay:
		return "Relay"
	case StrategyAll:
		return "Try All"
	default:
		return "Unknown"
	}
}

// NATInfo 节点的 NAT 信息
type NATInfo struct {
	Type            NATType
	PublicAddr      *net.UDPAddr
	PortPredictable bool
	PortDelta       int
	LastProbe       time.Time
}

// Encode 编码 NAT 信息
func (n *NATInfo) Encode() []byte {
	// 格式: [Type(1)] [Port(2)] [IPLen(1)] [IP(N)] [Predictable(1)] [Delta(2)]
	ip := n.PublicAddr.IP.To4()
	if ip == nil {
		ip = n.PublicAddr.IP.To16()
	}
	ipLen := len(ip)

	data := make([]byte, 7+ipLen)
	data[0] = byte(n.Type)
	binary.BigEndian.PutUint16(data[1:3], uint16(n.PublicAddr.Port))
	data[3] = byte(ipLen)
	copy(data[4:4+ipLen], ip)
	if n.PortPredictable {
		data[4+ipLen] = 1
	}
	binary.BigEndian.PutUint16(data[5+ipLen:7+ipLen], uint16(n.PortDelta))

	return data
}

// DecodeNATInfo 解码 NAT 信息
func DecodeNATInfo(data []byte) (*NATInfo, error) {
	if len(data) < 7 {
		return nil, fmt.Errorf("数据长度不足")
	}

	info := &NATInfo{
		Type: NATType(data[0]),
	}

	port := int(binary.BigEndian.Uint16(data[1:3]))
	ipLen := int(data[3])
	if len(data) < 7+ipLen {
		return nil, fmt.Errorf("数据长度不足，无法解析 IP")
	}

	ip := net.IP(data[4 : 4+ipLen])
	info.PublicAddr = &net.UDPAddr{IP: ip, Port: port}
	info.PortPredictable = data[4+ipLen] == 1
	info.PortDelta = int(binary.BigEndian.Uint16(data[5+ipLen : 7+ipLen]))
	info.LastProbe = time.Now()

	return info, nil
}
