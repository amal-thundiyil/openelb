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
	"sort"
	"time"

	"github.com/openelb/openelb/pkg/speaker/bgp/config"
	"github.com/osrg/gobgp/pkg/packet/bgp"

	log "github.com/sirupsen/logrus"
)

const (
	DEFAULT_LOCAL_PREF = 100
)

type Bitmap struct {
	bitmap []uint64
}

func (b *Bitmap) Flag(i uint) {
	b.bitmap[i/64] |= 1 << uint(i%64)
}

func (b *Bitmap) Unflag(i uint) {
	b.bitmap[i/64] &^= 1 << uint(i%64)
}

func (b *Bitmap) GetFlag(i uint) bool {
	return b.bitmap[i/64]&(1<<uint(i%64)) > 0
}

func (b *Bitmap) FindandSetZeroBit() (uint, error) {
	for i := 0; i < len(b.bitmap); i++ {
		if b.bitmap[i] == math.MaxUint64 {
			continue
		}
		// replace this with TrailingZero64() when gobgp drops go 1.8 support.
		for j := 0; j < 64; j++ {
			v := ^b.bitmap[i]
			if v&(1<<uint64(j)) > 0 {
				r := i*64 + j
				b.Flag(uint(r))
				return uint(r), nil
			}
		}
	}
	return 0, fmt.Errorf("no space")
}

func (b *Bitmap) Expand() {
	old := b.bitmap
	new := make([]uint64, len(old)+1)
	for i := 0; i < len(old); i++ {
		new[i] = old[i]
	}
	b.bitmap = new
}

func NewBitmap(size int) *Bitmap {
	b := &Bitmap{}
	if size != 0 {
		b.bitmap = make([]uint64, (size+64-1)/64)
	}
	return b
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

type RpkiValidationReasonType string

const (
	RPKI_VALIDATION_REASON_TYPE_NONE   RpkiValidationReasonType = "none"
	RPKI_VALIDATION_REASON_TYPE_AS     RpkiValidationReasonType = "as"
	RPKI_VALIDATION_REASON_TYPE_LENGTH RpkiValidationReasonType = "length"
)

var RpkiValidationReasonTypeToIntMap = map[RpkiValidationReasonType]int{
	RPKI_VALIDATION_REASON_TYPE_NONE:   0,
	RPKI_VALIDATION_REASON_TYPE_AS:     1,
	RPKI_VALIDATION_REASON_TYPE_LENGTH: 2,
}

func (v RpkiValidationReasonType) ToInt() int {
	i, ok := RpkiValidationReasonTypeToIntMap[v]
	if !ok {
		return -1
	}
	return i
}

var IntToRpkiValidationReasonTypeMap = map[int]RpkiValidationReasonType{
	0: RPKI_VALIDATION_REASON_TYPE_NONE,
	1: RPKI_VALIDATION_REASON_TYPE_AS,
	2: RPKI_VALIDATION_REASON_TYPE_LENGTH,
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

var localSource = &PeerInfo{}

func NewPath(source *PeerInfo, nlri bgp.AddrPrefixInterface, isWithdraw bool, pattrs []bgp.PathAttributeInterface, timestamp time.Time, noImplicitWithdraw bool) *Path {
	if source == nil {
		source = localSource
	}
	if !isWithdraw && pattrs == nil {
		log.WithFields(log.Fields{
			"Topic": "Table",
			"Key":   nlri.String(),
		}).Error("Need to provide path attributes for non-withdrawn path.")
		return nil
	}

	return &Path{
		info: &originInfo{
			nlri:               nlri,
			source:             source,
			timestamp:          timestamp.Unix(),
			noImplicitWithdraw: noImplicitWithdraw,
		},
		IsWithdraw: isWithdraw,
		pathAttrs:  pattrs,
	}
}

func NewEOR(family bgp.RouteFamily) *Path {
	afi, safi := bgp.RouteFamilyToAfiSafi(family)
	nlri, _ := bgp.NewPrefixFromRouteFamily(afi, safi)
	return &Path{
		info: &originInfo{
			nlri: nlri,
			eor:  true,
		},
	}
}

func (path *Path) IsEOR() bool {
	if path.info != nil && path.info.eor {
		return true
	}
	return false
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

func (path *Path) GetTimestamp() time.Time {
	return time.Unix(path.OriginInfo().timestamp, 0)
}

func (path *Path) setTimestamp(t time.Time) {
	path.OriginInfo().timestamp = t.Unix()
}

func (path *Path) IsLocal() bool {
	return path.GetSource().Address == nil
}

func (path *Path) IsIBGP() bool {
	as := path.GetSource().AS
	return (as == path.GetSource().LocalAS) && as != 0
}

// create new PathAttributes
func (path *Path) Clone(isWithdraw bool) *Path {
	return &Path{
		parent:           path,
		IsWithdraw:       isWithdraw,
		IsNexthopInvalid: path.IsNexthopInvalid,
		attrsHash:        path.attrsHash,
	}
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

func (path *Path) NoImplicitWithdraw() bool {
	return path.OriginInfo().noImplicitWithdraw
}

func (path *Path) IsFromExternal() bool {
	return path.OriginInfo().isFromExternal
}

func (path *Path) SetIsFromExternal(y bool) {
	path.OriginInfo().isFromExternal = y
}

func (path *Path) GetRouteFamily() bgp.RouteFamily {
	return bgp.AfiSafiToRouteFamily(path.OriginInfo().nlri.AFI(), path.OriginInfo().nlri.SAFI())
}

func (path *Path) GetSource() *PeerInfo {
	return path.OriginInfo().source
}

func (path *Path) MarkStale(s bool) {
	path.OriginInfo().stale = s
}

func (path *Path) IsStale() bool {
	return path.OriginInfo().stale
}

func (path *Path) IsRejected() bool {
	return path.rejected
}

func (path *Path) SetRejected(y bool) {
	path.rejected = y
}

func (path *Path) IsDropped() bool {
	return path.dropped
}

func (path *Path) SetDropped(y bool) {
	path.dropped = y
}

func (path *Path) HasNoLLGR() bool {
	for _, c := range path.GetCommunities() {
		if c == uint32(bgp.COMMUNITY_NO_LLGR) {
			return true
		}
	}
	return false
}

func (path *Path) IsLLGRStale() bool {
	for _, c := range path.GetCommunities() {
		if c == uint32(bgp.COMMUNITY_LLGR_STALE) {
			return true
		}
	}
	return false
}

func (path *Path) GetSourceAs() uint32 {
	attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_AS_PATH)
	if attr != nil {
		asPathParam := attr.(*bgp.PathAttributeAsPath).Value
		if len(asPathParam) == 0 {
			return 0
		}
		asList := asPathParam[len(asPathParam)-1].GetAS()
		if len(asList) == 0 {
			return 0
		}
		return asList[len(asList)-1]
	}
	return 0
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

type PathAttrs []bgp.PathAttributeInterface

func (a PathAttrs) Len() int {
	return len(a)
}

func (a PathAttrs) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a PathAttrs) Less(i, j int) bool {
	return a[i].GetType() < a[j].GetType()
}

func (path *Path) GetPathAttrs() []bgp.PathAttributeInterface {
	deleted := NewBitmap(math.MaxUint8)
	modified := make(map[uint]bgp.PathAttributeInterface)
	p := path
	for {
		for _, t := range p.dels {
			deleted.Flag(uint(t))
		}
		if p.parent == nil {
			list := PathAttrs(make([]bgp.PathAttributeInterface, 0, len(p.pathAttrs)))
			// we assume that the original pathAttrs are
			// in order, that is, other bgp speakers send
			// attributes in order.
			for _, a := range p.pathAttrs {
				typ := uint(a.GetType())
				if m, ok := modified[typ]; ok {
					list = append(list, m)
					delete(modified, typ)
				} else if !deleted.GetFlag(typ) {
					list = append(list, a)
				}
			}
			if len(modified) > 0 {
				// Huh, some attributes were newly
				// added. So we need to sort...
				for _, m := range modified {
					list = append(list, m)
				}
				sort.Sort(list)
			}
			return list
		} else {
			for _, a := range p.pathAttrs {
				typ := uint(a.GetType())
				if _, ok := modified[typ]; !deleted.GetFlag(typ) && !ok {
					modified[typ] = a
				}
			}
		}
		p = p.parent
	}
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

// return Path's string representation
func (path *Path) String() string {
	s := bytes.NewBuffer(make([]byte, 0, 64))
	if path.IsEOR() {
		s.WriteString(fmt.Sprintf("{ %s EOR | src: %s }", path.GetRouteFamily(), path.GetSource()))
		return s.String()
	}
	s.WriteString(fmt.Sprintf("{ %s | ", path.getPrefix()))
	s.WriteString(fmt.Sprintf("src: %s", path.GetSource()))
	s.WriteString(fmt.Sprintf(", nh: %s", path.GetNexthop()))
	if path.IsNexthopInvalid {
		s.WriteString(" (not reachable)")
	}
	if path.IsWithdraw {
		s.WriteString(", withdraw")
	}
	s.WriteString(" }")
	return s.String()
}

func (path *Path) getPrefix() string {
	return path.GetNlri().String()
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

func (path *Path) GetAsList() []uint32 {
	return path.getAsListOfSpecificType(true, true)

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

func (path *Path) GetLabelString() string {
	return bgp.LabelString(path.GetNlri())
}

// PrependAsn prepends AS number.
// This function updates the AS_PATH attribute as follows.
// (If the peer is in the confederation member AS,
//  replace AS_SEQUENCE in the following sentence with AS_CONFED_SEQUENCE.)
//  1) if the first path segment of the AS_PATH is of type
//     AS_SEQUENCE, the local system prepends the specified AS num as
//     the last element of the sequence (put it in the left-most
//     position with respect to the position of  octets in the
//     protocol message) the specified number of times.
//     If the act of prepending will cause an overflow in the AS_PATH
//     segment (i.e.,  more than 255 ASes),
//     it SHOULD prepend a new segment of type AS_SEQUENCE
//     and prepend its own AS number to this new segment.
//
//  2) if the first path segment of the AS_PATH is of other than type
//     AS_SEQUENCE, the local system prepends a new path segment of type
//     AS_SEQUENCE to the AS_PATH, including the specified AS number in
//     that segment.
//
//  3) if the AS_PATH is empty, the local system creates a path
//     segment of type AS_SEQUENCE, places the specified AS number
//     into that segment, and places that segment into the AS_PATH.
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

func isPrivateAS(as uint32) bool {
	return (64512 <= as && as <= 65534) || (4200000000 <= as && as <= 4294967294)
}

func (path *Path) RemovePrivateAS(localAS uint32, option config.RemovePrivateAsOption) {
	original := path.GetAsPath()
	if original == nil {
		return
	}
	switch option {
	case config.REMOVE_PRIVATE_AS_OPTION_ALL, config.REMOVE_PRIVATE_AS_OPTION_REPLACE:
		newASParams := make([]bgp.AsPathParamInterface, 0, len(original.Value))
		for _, param := range original.Value {
			asList := param.GetAS()
			newASParam := make([]uint32, 0, len(asList))
			for _, as := range asList {
				if isPrivateAS(as) {
					if option == config.REMOVE_PRIVATE_AS_OPTION_REPLACE {
						newASParam = append(newASParam, localAS)
					}
				} else {
					newASParam = append(newASParam, as)
				}
			}
			if len(newASParam) > 0 {
				newASParams = append(newASParams, bgp.NewAs4PathParam(param.GetType(), newASParam))
			}
		}
		path.setPathAttr(bgp.NewPathAttributeAsPath(newASParams))
	}
}

func (path *Path) removeConfedAs() {
	original := path.GetAsPath()
	if original == nil {
		return
	}
	newAsParams := make([]bgp.AsPathParamInterface, 0, len(original.Value))
	for _, param := range original.Value {
		switch param.GetType() {
		case bgp.BGP_ASPATH_ATTR_TYPE_SEQ, bgp.BGP_ASPATH_ATTR_TYPE_SET:
			newAsParams = append(newAsParams, param)
		}
	}
	path.setPathAttr(bgp.NewPathAttributeAsPath(newAsParams))
}

func (path *Path) ReplaceAS(localAS, peerAS uint32) *Path {
	original := path.GetAsPath()
	if original == nil {
		return path
	}
	newASParams := make([]bgp.AsPathParamInterface, 0, len(original.Value))
	changed := false
	for _, param := range original.Value {
		segType := param.GetType()
		asList := param.GetAS()
		newASParam := make([]uint32, 0, len(asList))
		for _, as := range asList {
			if as == peerAS {
				as = localAS
				changed = true
			}
			newASParam = append(newASParam, as)
		}
		newASParams = append(newASParams, bgp.NewAs4PathParam(segType, newASParam))
	}
	if changed {
		path = path.Clone(path.IsWithdraw)
		path.setPathAttr(bgp.NewPathAttributeAsPath(newASParams))
	}
	return path
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

// RemoveCommunities removes specific communities.
// If the length of communities is 0, it does nothing.
// If all communities are removed, it removes Communities path attribute itself.
func (path *Path) RemoveCommunities(communities []uint32) int {

	if len(communities) == 0 {
		// do nothing
		return 0
	}

	find := func(val uint32) bool {
		for _, com := range communities {
			if com == val {
				return true
			}
		}
		return false
	}

	count := 0
	attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_COMMUNITIES)
	if attr != nil {
		newList := make([]uint32, 0)
		c := attr.(*bgp.PathAttributeCommunities)

		for _, value := range c.Value {
			if find(value) {
				count += 1
			} else {
				newList = append(newList, value)
			}
		}

		if len(newList) != 0 {
			path.setPathAttr(bgp.NewPathAttributeCommunities(newList))
		} else {
			path.delPathAttr(bgp.BGP_ATTR_TYPE_COMMUNITIES)
		}
	}
	return count
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

func (path *Path) GetMed() (uint32, error) {
	attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_MULTI_EXIT_DISC)
	if attr == nil {
		return 0, fmt.Errorf("no med path attr")
	}
	return attr.(*bgp.PathAttributeMultiExitDisc).Value, nil
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

func (path *Path) RemoveLocalPref() {
	if path.getPathAttr(bgp.BGP_ATTR_TYPE_LOCAL_PREF) != nil {
		path.delPathAttr(bgp.BGP_ATTR_TYPE_LOCAL_PREF)
	}
}

func (path *Path) GetOriginatorID() net.IP {
	if attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_ORIGINATOR_ID); attr != nil {
		return attr.(*bgp.PathAttributeOriginatorId).Value
	}
	return nil
}

func (path *Path) GetClusterList() []net.IP {
	if attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_CLUSTER_LIST); attr != nil {
		return attr.(*bgp.PathAttributeClusterList).Value
	}
	return nil
}

func (path *Path) GetOrigin() (uint8, error) {
	if attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_ORIGIN); attr != nil {
		return attr.(*bgp.PathAttributeOrigin).Value, nil
	}
	return 0, fmt.Errorf("no origin path attr")
}

func (path *Path) GetLocalPref() (uint32, error) {
	lp := uint32(DEFAULT_LOCAL_PREF)
	attr := path.getPathAttr(bgp.BGP_ATTR_TYPE_LOCAL_PREF)
	if attr != nil {
		lp = attr.(*bgp.PathAttributeLocalPref).Value
	}
	return lp, nil
}

func (lhs *Path) Equal(rhs *Path) bool {
	if rhs == nil {
		return false
	}

	if !lhs.GetSource().Equal(rhs.GetSource()) {
		return false
	}

	pattrs := func(arg []bgp.PathAttributeInterface) []byte {
		ret := make([]byte, 0)
		for _, a := range arg {
			aa, _ := a.Serialize()
			ret = append(ret, aa...)
		}
		return ret
	}
	return bytes.Equal(pattrs(lhs.GetPathAttrs()), pattrs(rhs.GetPathAttrs()))
}

func (lhs *Path) Compare(rhs *Path) int {
	if lhs.IsLocal() && !rhs.IsLocal() {
		return 1
	} else if !lhs.IsLocal() && rhs.IsLocal() {
		return -1
	}

	if !lhs.IsIBGP() && rhs.IsIBGP() {
		return 1
	} else if lhs.IsIBGP() && !rhs.IsIBGP() {
		return -1
	}

	lp1, _ := lhs.GetLocalPref()
	lp2, _ := rhs.GetLocalPref()
	if lp1 != lp2 {
		return int(lp1 - lp2)
	}

	l1 := lhs.GetAsPathLen()
	l2 := rhs.GetAsPathLen()
	if l1 != l2 {
		return int(l2 - l1)
	}

	o1, _ := lhs.GetOrigin()
	o2, _ := rhs.GetOrigin()
	if o1 != o2 {
		return int(o2 - o1)
	}

	m1, _ := lhs.GetMed()
	m2, _ := rhs.GetMed()
	return int(m2 - m1)
}

func (p *Path) ToLocal() *Path {
	nlri := p.GetNlri()
	f := p.GetRouteFamily()
	pathId := nlri.PathLocalIdentifier()
	switch f {
	case bgp.RF_IPv4_VPN:
		n := nlri.(*bgp.LabeledVPNIPAddrPrefix)
		_, c, _ := net.ParseCIDR(n.IPPrefix())
		ones, _ := c.Mask.Size()
		nlri = bgp.NewIPAddrPrefix(uint8(ones), c.IP.String())
		nlri.SetPathLocalIdentifier(pathId)
	case bgp.RF_FS_IPv4_VPN:
		n := nlri.(*bgp.FlowSpecIPv4VPN)
		nlri = bgp.NewFlowSpecIPv4Unicast(n.FlowSpecNLRI.Value)
		nlri.SetPathLocalIdentifier(pathId)
	case bgp.RF_IPv6_VPN:
		n := nlri.(*bgp.LabeledVPNIPv6AddrPrefix)
		_, c, _ := net.ParseCIDR(n.IPPrefix())
		ones, _ := c.Mask.Size()
		nlri = bgp.NewIPv6AddrPrefix(uint8(ones), c.IP.String())
		nlri.SetPathLocalIdentifier(pathId)
	case bgp.RF_FS_IPv6_VPN:
		n := nlri.(*bgp.FlowSpecIPv6VPN)
		nlri = bgp.NewFlowSpecIPv6Unicast(n.FlowSpecNLRI.Value)
		nlri.SetPathLocalIdentifier(pathId)
	default:
		return p
	}
	path := NewPath(p.OriginInfo().source, nlri, p.IsWithdraw, p.GetPathAttrs(), p.GetTimestamp(), false)
	switch f {
	case bgp.RF_IPv4_VPN, bgp.RF_IPv6_VPN:
		path.delPathAttr(bgp.BGP_ATTR_TYPE_EXTENDED_COMMUNITIES)
	case bgp.RF_FS_IPv4_VPN, bgp.RF_FS_IPv6_VPN:
		extcomms := path.GetExtCommunities()
		newExtComms := make([]bgp.ExtendedCommunityInterface, 0, len(extcomms))
		for _, extComm := range extcomms {
			_, subType := extComm.GetTypes()
			if subType == bgp.EC_SUBTYPE_ROUTE_TARGET {
				continue
			}
			newExtComms = append(newExtComms, extComm)
		}
		path.SetExtCommunities(newExtComms, true)
	}

	if f == bgp.RF_IPv4_VPN {
		nh := path.GetNexthop()
		path.delPathAttr(bgp.BGP_ATTR_TYPE_MP_REACH_NLRI)
		path.setPathAttr(bgp.NewPathAttributeNextHop(nh.String()))
	}
	path.IsNexthopInvalid = p.IsNexthopInvalid
	return path
}

func (p *Path) SetHash(v uint32) {
	p.attrsHash = v
}

func (p *Path) GetHash() uint32 {
	return p.attrsHash
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
