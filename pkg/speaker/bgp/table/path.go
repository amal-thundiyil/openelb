package table

import (
	"net"

	"github.com/openelb/openelb/pkg/speaker/bgp/config"
	"github.com/osrg/gobgp/pkg/packet/bgp"
)

type Path struct {
	info      *originInfo
	parent    *Path
	pathAttrs []bgp.PathAttributeInterface
	dels      []bgp.BGPAttrType
	attrsHash uint32
	rejected  bool
	// doesn't exist in the adj
	dropped bool

	// For BGP Nexthop Tracking, this field shows if nexthop is invalidated by IGP.
	IsNexthopInvalid bool
	IsWithdraw       bool
}

type originInfo struct {
	nlri               bgp.AddrPrefixInterface
	source             *PeerInfo
	timestamp          int64
	noImplicitWithdraw bool
	isFromExternal     bool
	eor                bool
	stale              bool
}

type PeerInfo struct {
	AS                      uint32
	ID                      net.IP
	LocalAS                 uint32
	LocalID                 net.IP
	Address                 net.IP
	LocalAddress            net.IP
	RouteReflectorClient    bool
	RouteReflectorClusterID net.IP
	MultihopTtl             uint8
	Confederation           bool
}

type Validation struct {
	Status          config.RpkiValidationResultType
	Reason          RpkiValidationReasonType
	Matched         []*ROA
	UnmatchedAs     []*ROA
	UnmatchedLength []*ROA
}

type ROA struct {
	Family  int
	Network *net.IPNet
	MaxLen  uint8
	AS      uint32
	Src     string
}

type RpkiValidationReasonType string

// create new PathAttributes
func (path *Path) Clone(isWithdraw bool) *Path {
	return &Path{
		parent:           path,
		IsWithdraw:       isWithdraw,
		IsNexthopInvalid: path.IsNexthopInvalid,
		attrsHash:        path.attrsHash,
	}
}

func (path *Path) GetRouteFamily() bgp.RouteFamily {
	return bgp.AfiSafiToRouteFamily(path.OriginInfo().nlri.AFI(), path.OriginInfo().nlri.SAFI())
}

func (path *Path) OriginInfo() *originInfo {
	return path.root().info
}

func (path *Path) root() *Path {
	p := path
	for p.parent != nil {
		p = p.parent
	}
	return p
}

func nlriToIPNet(nlri bgp.AddrPrefixInterface) *net.IPNet {
	switch T := nlri.(type) {
	case *bgp.IPAddrPrefix:
		return &net.IPNet{
			IP:   net.IP(T.Prefix.To4()),
			Mask: net.CIDRMask(int(T.Length), 32),
		}
	case *bgp.IPv6AddrPrefix:
		return &net.IPNet{
			IP:   net.IP(T.Prefix.To16()),
			Mask: net.CIDRMask(int(T.Length), 128),
		}
	case *bgp.LabeledIPAddrPrefix:
		return &net.IPNet{
			IP:   net.IP(T.Prefix.To4()),
			Mask: net.CIDRMask(int(T.Length), 32),
		}
	case *bgp.LabeledIPv6AddrPrefix:
		return &net.IPNet{
			IP:   net.IP(T.Prefix.To4()),
			Mask: net.CIDRMask(int(T.Length), 128),
		}
	}
	return nil
}

func (path *Path) GetNlri() bgp.AddrPrefixInterface {
	return path.OriginInfo().nlri
}
