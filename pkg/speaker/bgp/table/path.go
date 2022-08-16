// Copyright (C) 2014 Nippon Telegraph and Telephone Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package table

import (
	"bytes"
	"fmt"
	"math"
	"net"

	"github.com/osrg/gobgp/pkg/packet/bgp"
)

type originInfo struct {
	nlri               bgp.AddrPrefixInterface
	source             *PeerInfo
	timestamp          int64
	noImplicitWithdraw bool
	isFromExternal     bool
	eor                bool
	stale              bool
}

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

func cloneAsPath(asAttr *bgp.PathAttributeAsPath) *bgp.PathAttributeAsPath {
	newASparams := make([]bgp.AsPathParamInterface, len(asAttr.Value))
	for i, param := range asAttr.Value {
		asList := param.GetAS()
		as := make([]uint32, len(asList))
		copy(as, asList)
		newASparams[i] = bgp.NewAs4PathParam(param.GetType(), as)
	}
	return bgp.NewPathAttributeAsPath(newASparams)
}

func (path *Path) IsLocal() bool {
	return path.GetSource().Address == nil
}

func (path *Path) IsIBGP() bool {
	as := path.GetSource().AS
	return (as == path.GetSource().LocalAS) && as != 0
}

func (path *Path) root() *Path {
	p := path
	for p.parent != nil {
		p = p.parent
	}
	return p
}

func (path *Path) OriginInfo() *originInfo {
	return path.root().info
}

func (path *Path) GetRouteFamily() bgp.RouteFamily {
	return bgp.AfiSafiToRouteFamily(path.OriginInfo().nlri.AFI(), path.OriginInfo().nlri.SAFI())
}

func (path *Path) GetSource() *PeerInfo {
	return path.OriginInfo().source
}

func (path *Path) GetNexthop() net.IP {
	attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_NEXT_HOP)
	if attr != nil {
		return attr.(*bgp.PathAttributeNextHop).Value
	}
	attr = path.getPathAttr(bgp.BGP_ATTR_TYPE_MP_REACH_NLRI)
	if attr != nil {
		return attr.(*bgp.PathAttributeMpReachNLRI).Nexthop
	}
	return net.IP{}
}

func (path *Path) SetNexthop(nexthop net.IP) {
	if path.GetRouteFamily() == bgp.RF_IPv4_UC && nexthop.To4() == nil {
		path.delPathAttr(bgp.BGP_ATTR_TYPE_NEXT_HOP)
		mpreach := bgp.NewPathAttributeMpReachNLRI(nexthop.String(), []bgp.AddrPrefixInterface{path.GetNlri()})
		path.setPathAttr(mpreach)
		return
	}
	attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_NEXT_HOP)
	if attr != nil {
		path.setPathAttr(bgp.NewPathAttributeNextHop(nexthop.String()))
	}
	attr = path.getPathAttr(bgp.BGP_ATTR_TYPE_MP_REACH_NLRI)
	if attr != nil {
		oldNlri := attr.(*bgp.PathAttributeMpReachNLRI)
		path.setPathAttr(bgp.NewPathAttributeMpReachNLRI(nexthop.String(), oldNlri.Value))
	}
}

func (path *Path) GetNlri() bgp.AddrPrefixInterface {
	return path.OriginInfo().nlri
}

func (path *Path) getPathAttr(typ bgp.BGPAttrType) bgp.PathAttributeInterface {
	p := path
	for {
		for _, t := range p.dels {
			if t == typ {
				return nil
			}
		}
		for _, a := range p.pathAttrs {
			if a.GetType() == typ {
				return a
			}
		}
		if p.parent == nil {
			return nil
		}
		p = p.parent
	}
}

func (path *Path) setPathAttr(a bgp.PathAttributeInterface) {
	if len(path.pathAttrs) == 0 {
		path.pathAttrs = []bgp.PathAttributeInterface{a}
	} else {
		for i, b := range path.pathAttrs {
			if a.GetType() == b.GetType() {
				path.pathAttrs[i] = a
				return
			}
		}
		path.pathAttrs = append(path.pathAttrs, a)
	}
}

func (path *Path) delPathAttr(typ bgp.BGPAttrType) {
	if len(path.dels) == 0 {
		path.dels = []bgp.BGPAttrType{typ}
	} else {
		path.dels = append(path.dels, typ)
	}
}
func (path *Path) GetAsPath() *bgp.PathAttributeAsPath {
	attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_AS_PATH)
	if attr != nil {
		return attr.(*bgp.PathAttributeAsPath)
	}
	return nil
}

// GetAsPathLen returns the number of AS_PATH
func (path *Path) GetAsPathLen() int {

	var length int = 0
	if aspath := path.GetAsPath(); aspath != nil {
		for _, as := range aspath.Value {
			length += as.ASLen()
		}
	}
	return length
}

func (path *Path) GetAsString() string {
	s := bytes.NewBuffer(make([]byte, 0, 64))
	if aspath := path.GetAsPath(); aspath != nil {
		return bgp.AsPathString(aspath)
	}
	return s.String()
}

func (path *Path) GetAsSeqList() []uint32 {
	return path.getAsListOfSpecificType(true, false)

}

func (path *Path) getAsListOfSpecificType(getAsSeq, getAsSet bool) []uint32 {
	asList := []uint32{}
	if aspath := path.GetAsPath(); aspath != nil {
		for _, param := range aspath.Value {
			segType := param.GetType()
			if getAsSeq && segType == bgp.BGP_ASPATH_ATTR_TYPE_SEQ {
				asList = append(asList, param.GetAS()...)
				continue
			}
			if getAsSet && segType == bgp.BGP_ASPATH_ATTR_TYPE_SET {
				asList = append(asList, param.GetAS()...)
			} else {
				asList = append(asList, 0)
			}
		}
	}
	return asList
}

func (path *Path) PrependAsn(asn uint32, repeat uint8, confed bool) {
	var segType uint8
	if confed {
		segType = bgp.BGP_ASPATH_ATTR_TYPE_CONFED_SEQ
	} else {
		segType = bgp.BGP_ASPATH_ATTR_TYPE_SEQ
	}

	original := path.GetAsPath()

	asns := make([]uint32, repeat)
	for i := range asns {
		asns[i] = asn
	}

	var asPath *bgp.PathAttributeAsPath
	if original == nil {
		asPath = bgp.NewPathAttributeAsPath([]bgp.AsPathParamInterface{})
	} else {
		asPath = cloneAsPath(original)
	}

	if len(asPath.Value) > 0 {
		param := asPath.Value[0]
		asList := param.GetAS()
		if param.GetType() == segType {
			if int(repeat)+len(asList) > 255 {
				repeat = uint8(255 - len(asList))
			}
			newAsList := append(asns[:int(repeat)], asList...)
			asPath.Value[0] = bgp.NewAs4PathParam(segType, newAsList)
			asns = asns[int(repeat):]
		}
	}

	if len(asns) > 0 {
		p := bgp.NewAs4PathParam(segType, asns)
		asPath.Value = append([]bgp.AsPathParamInterface{p}, asPath.Value...)
	}
	path.setPathAttr(asPath)
}

func (path *Path) GetCommunities() []uint32 {
	communityList := []uint32{}
	if attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_COMMUNITIES); attr != nil {
		communities := attr.(*bgp.PathAttributeCommunities)
		communityList = append(communityList, communities.Value...)
	}
	return communityList
}

// SetCommunities adds or replaces communities with new ones.
// If the length of communities is 0 and doReplace is true, it clears communities.
func (path *Path) SetCommunities(communities []uint32, doReplace bool) {

	if len(communities) == 0 && doReplace {
		// clear communities
		path.delPathAttr(bgp.BGP_ATTR_TYPE_COMMUNITIES)
		return
	}

	newList := make([]uint32, 0)
	attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_COMMUNITIES)
	if attr != nil {
		c := attr.(*bgp.PathAttributeCommunities)
		if doReplace {
			newList = append(newList, communities...)
		} else {
			newList = append(newList, c.Value...)
			newList = append(newList, communities...)
		}
	} else {
		newList = append(newList, communities...)
	}
	path.setPathAttr(bgp.NewPathAttributeCommunities(newList))

}

func (path *Path) GetExtCommunities() []bgp.ExtendedCommunityInterface {
	eCommunityList := make([]bgp.ExtendedCommunityInterface, 0)
	if attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_EXTENDED_COMMUNITIES); attr != nil {
		eCommunities := attr.(*bgp.PathAttributeExtendedCommunities).Value
		eCommunityList = append(eCommunityList, eCommunities...)
	}
	return eCommunityList
}

func (path *Path) SetExtCommunities(exts []bgp.ExtendedCommunityInterface, doReplace bool) {
	attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_EXTENDED_COMMUNITIES)
	if attr != nil {
		l := attr.(*bgp.PathAttributeExtendedCommunities).Value
		if doReplace {
			l = exts
		} else {
			l = append(l, exts...)
		}
		path.setPathAttr(bgp.NewPathAttributeExtendedCommunities(l))
	} else {
		path.setPathAttr(bgp.NewPathAttributeExtendedCommunities(exts))
	}
}

func (path *Path) GetLargeCommunities() []*bgp.LargeCommunity {
	if a := path.getPathAttr(bgp.BGP_ATTR_TYPE_LARGE_COMMUNITY); a != nil {
		v := a.(*bgp.PathAttributeLargeCommunities).Values
		ret := make([]*bgp.LargeCommunity, 0, len(v))
		ret = append(ret, v...)
		return ret
	}
	return nil
}

func (path *Path) SetLargeCommunities(cs []*bgp.LargeCommunity, doReplace bool) {
	if len(cs) == 0 && doReplace {
		// clear large communities
		path.delPathAttr(bgp.BGP_ATTR_TYPE_LARGE_COMMUNITY)
		return
	}

	a := path.getPathAttr(bgp.BGP_ATTR_TYPE_LARGE_COMMUNITY)
	if a == nil || doReplace {
		path.setPathAttr(bgp.NewPathAttributeLargeCommunities(cs))
	} else {
		l := a.(*bgp.PathAttributeLargeCommunities).Values
		path.setPathAttr(bgp.NewPathAttributeLargeCommunities(append(l, cs...)))
	}
}

// SetMed replace, add or subtraction med with new ones.
func (path *Path) SetMed(med int64, doReplace bool) error {
	parseMed := func(orgMed uint32, med int64, doReplace bool) (*bgp.PathAttributeMultiExitDisc, error) {
		if doReplace {
			return bgp.NewPathAttributeMultiExitDisc(uint32(med)), nil
		}

		medVal := int64(orgMed) + med
		if medVal < 0 {
			return nil, fmt.Errorf("med value invalid. it's underflow threshold: %v", medVal)
		} else if medVal > int64(math.MaxUint32) {
			return nil, fmt.Errorf("med value invalid. it's overflow threshold: %v", medVal)
		}

		return bgp.NewPathAttributeMultiExitDisc(uint32(int64(orgMed) + med)), nil
	}

	m := uint32(0)
	if attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_MULTI_EXIT_DISC); attr != nil {
		m = attr.(*bgp.PathAttributeMultiExitDisc).Value
	}
	newMed, err := parseMed(m, med, doReplace)
	if err != nil {
		return err
	}
	path.setPathAttr(newMed)
	return nil
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