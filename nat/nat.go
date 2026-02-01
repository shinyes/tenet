package nat

// NATType NAT类型
type NATType int

const (
	NATUnknown        NATType = iota
	NATNone                   // 公网IP
	NATFullCone               // 全锥形NAT
	NATRestrictedCone         // 受限锥形NAT
	NATPortRestricted         // 端口受限锥形NAT
	NATSymmetric              // 对称型NAT（最难穿透）
)

func (t NATType) String() string {
	switch t {
	case NATNone:
		return "Public"
	case NATFullCone:
		return "Full Cone"
	case NATRestrictedCone:
		return "Restricted Cone"
	case NATPortRestricted:
		return "Port Restricted Cone"
	case NATSymmetric:
		return "Symmetric"
	default:
		return "Unknown"
	}
}
