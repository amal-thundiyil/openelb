// Copyright (C) 2014-2016 Nippon Telegraph and Telephone Corporation.
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
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/k-sone/critbitgo"
	"github.com/openelb/openelb/pkg/speaker/bgp/config"
	api "github.com/osrg/gobgp/api"
	"github.com/osrg/gobgp/pkg/packet/bgp"
	log "github.com/sirupsen/logrus"
)

const (
	GLOBAL_RIB_NAME = "global"
)

type PolicyOptions struct {
	Info       *PeerInfo
	OldNextHop net.IP
	Validate   func(*Path) *Validation
}

type DefinedType int

const (
	DEFINED_TYPE_PREFIX DefinedType = iota
	DEFINED_TYPE_NEIGHBOR
	DEFINED_TYPE_TAG
	DEFINED_TYPE_AS_PATH
	DEFINED_TYPE_COMMUNITY
	DEFINED_TYPE_EXT_COMMUNITY
	DEFINED_TYPE_LARGE_COMMUNITY
	DEFINED_TYPE_NEXT_HOP
)

type RouteType int

const (
	ROUTE_TYPE_NONE RouteType = iota
	ROUTE_TYPE_ACCEPT
	ROUTE_TYPE_REJECT
)

func (t RouteType) String() string {
	switch t {
	case ROUTE_TYPE_NONE:
		return "continue"
	case ROUTE_TYPE_ACCEPT:
		return "accept"
	case ROUTE_TYPE_REJECT:
		return "reject"
	}
	return fmt.Sprintf("unknown(%d)", t)
}

type PolicyDirection int

const (
	POLICY_DIRECTION_NONE PolicyDirection = iota
	POLICY_DIRECTION_IMPORT
	POLICY_DIRECTION_EXPORT
)

func (d PolicyDirection) String() string {
	switch d {
	case POLICY_DIRECTION_IMPORT:
		return "import"
	case POLICY_DIRECTION_EXPORT:
		return "export"
	}
	return fmt.Sprintf("unknown(%d)", d)
}

type MatchOption int

const (
	MATCH_OPTION_ANY MatchOption = iota
	MATCH_OPTION_ALL
	MATCH_OPTION_INVERT
)

func (o MatchOption) String() string {
	switch o {
	case MATCH_OPTION_ANY:
		return "any"
	case MATCH_OPTION_ALL:
		return "all"
	case MATCH_OPTION_INVERT:
		return "invert"
	default:
		return fmt.Sprintf("MatchOption(%d)", o)
	}
}

func (o MatchOption) ConvertToMatchSetOptionsRestrictedType() config.MatchSetOptionsRestrictedType {
	switch o {
	case MATCH_OPTION_ANY:
		return config.MATCH_SET_OPTIONS_RESTRICTED_TYPE_ANY
	case MATCH_OPTION_INVERT:
		return config.MATCH_SET_OPTIONS_RESTRICTED_TYPE_INVERT
	}
	return "unknown"
}

type MedActionType int

const (
	MED_ACTION_MOD MedActionType = iota
	MED_ACTION_REPLACE
)

var CommunityOptionNameMap = map[config.BgpSetCommunityOptionType]string{
	config.BGP_SET_COMMUNITY_OPTION_TYPE_ADD:     "add",
	config.BGP_SET_COMMUNITY_OPTION_TYPE_REMOVE:  "remove",
	config.BGP_SET_COMMUNITY_OPTION_TYPE_REPLACE: "replace",
}

var CommunityOptionValueMap = map[string]config.BgpSetCommunityOptionType{
	CommunityOptionNameMap[config.BGP_SET_COMMUNITY_OPTION_TYPE_ADD]:     config.BGP_SET_COMMUNITY_OPTION_TYPE_ADD,
	CommunityOptionNameMap[config.BGP_SET_COMMUNITY_OPTION_TYPE_REMOVE]:  config.BGP_SET_COMMUNITY_OPTION_TYPE_REMOVE,
	CommunityOptionNameMap[config.BGP_SET_COMMUNITY_OPTION_TYPE_REPLACE]: config.BGP_SET_COMMUNITY_OPTION_TYPE_REPLACE,
}

type ConditionType int

const (
	CONDITION_PREFIX ConditionType = iota
	CONDITION_NEIGHBOR
	CONDITION_AS_PATH
	CONDITION_COMMUNITY
	CONDITION_EXT_COMMUNITY
	CONDITION_AS_PATH_LENGTH
	CONDITION_RPKI
	CONDITION_ROUTE_TYPE
	CONDITION_LARGE_COMMUNITY
	CONDITION_NEXT_HOP
	CONDITION_AFI_SAFI_IN
)

type ActionType int

const (
	ACTION_ROUTING ActionType = iota
	ACTION_COMMUNITY
	ACTION_EXT_COMMUNITY
	ACTION_MED
	ACTION_AS_PATH_PREPEND
	ACTION_NEXTHOP
	ACTION_LOCAL_PREF
	ACTION_LARGE_COMMUNITY
)

func NewMatchOption(c interface{}) (MatchOption, error) {
	switch t := c.(type) {
	case config.MatchSetOptionsType:
		t = t.DefaultAsNeeded()
		switch t {
		case config.MATCH_SET_OPTIONS_TYPE_ANY:
			return MATCH_OPTION_ANY, nil
		case config.MATCH_SET_OPTIONS_TYPE_ALL:
			return MATCH_OPTION_ALL, nil
		case config.MATCH_SET_OPTIONS_TYPE_INVERT:
			return MATCH_OPTION_INVERT, nil
		}
	case config.MatchSetOptionsRestrictedType:
		t = t.DefaultAsNeeded()
		switch t {
		case config.MATCH_SET_OPTIONS_RESTRICTED_TYPE_ANY:
			return MATCH_OPTION_ANY, nil
		case config.MATCH_SET_OPTIONS_RESTRICTED_TYPE_INVERT:
			return MATCH_OPTION_INVERT, nil
		}
	}
	return MATCH_OPTION_ANY, fmt.Errorf("invalid argument to create match option: %v", c)
}

type AttributeComparison int

const (
	// "== comparison"
	ATTRIBUTE_EQ AttributeComparison = iota
	// ">= comparison"
	ATTRIBUTE_GE
	// "<= comparison"
	ATTRIBUTE_LE
)

func (c AttributeComparison) String() string {
	switch c {
	case ATTRIBUTE_EQ:
		return "="
	case ATTRIBUTE_GE:
		return ">="
	case ATTRIBUTE_LE:
		return "<="
	}
	return "?"
}

const (
	ASPATH_REGEXP_MAGIC = "(^|[,{}() ]|$)"
)

type DefinedSet interface {
	Type() DefinedType
	Name() string
	Append(DefinedSet) error
	Remove(DefinedSet) error
	Replace(DefinedSet) error
	String() string
	List() []string
}

type DefinedSetMap map[DefinedType]map[string]DefinedSet

type DefinedSetList []DefinedSet

func (l DefinedSetList) Len() int {
	return len(l)
}

func (l DefinedSetList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l DefinedSetList) Less(i, j int) bool {
	if l[i].Type() != l[j].Type() {
		return l[i].Type() < l[j].Type()
	}
	return l[i].Name() < l[j].Name()
}

type Prefix struct {
	Prefix             *net.IPNet
	AddressFamily      bgp.RouteFamily
	MasklengthRangeMax uint8
	MasklengthRangeMin uint8
}

func (p *Prefix) Match(path *Path) bool {
	rf := path.GetRouteFamily()
	if rf != p.AddressFamily {
		return false
	}

	var pAddr net.IP
	var pMasklen uint8
	switch rf {
	case bgp.RF_IPv4_UC:
		pAddr = path.GetNlri().(*bgp.IPAddrPrefix).Prefix
		pMasklen = path.GetNlri().(*bgp.IPAddrPrefix).Length
	case bgp.RF_IPv6_UC:
		pAddr = path.GetNlri().(*bgp.IPv6AddrPrefix).Prefix
		pMasklen = path.GetNlri().(*bgp.IPv6AddrPrefix).Length
	default:
		return false
	}

	return (p.MasklengthRangeMin <= pMasklen && pMasklen <= p.MasklengthRangeMax) && p.Prefix.Contains(pAddr)
}

func (lhs *Prefix) Equal(rhs *Prefix) bool {
	if lhs == rhs {
		return true
	}
	if rhs == nil {
		return false
	}
	return lhs.Prefix.String() == rhs.Prefix.String() && lhs.MasklengthRangeMin == rhs.MasklengthRangeMin && lhs.MasklengthRangeMax == rhs.MasklengthRangeMax
}

func (p *Prefix) PrefixString() string {
	isZeros := func(p net.IP) bool {
		for i := 0; i < len(p); i++ {
			if p[i] != 0 {
				return false
			}
		}
		return true
	}

	ip := p.Prefix.IP
	if p.AddressFamily == bgp.RF_IPv6_UC && isZeros(ip[0:10]) && ip[10] == 0xff && ip[11] == 0xff {
		m, _ := p.Prefix.Mask.Size()
		return fmt.Sprintf("::FFFF:%s/%d", ip.To16(), m)
	}
	return p.Prefix.String()
}

var _regexpPrefixRange = regexp.MustCompile(`(\d+)\.\.(\d+)`)

type PrefixSet struct {
	name   string
	tree   *critbitgo.Net
	family bgp.RouteFamily
}

func (s *PrefixSet) Name() string {
	return s.name
}

func (s *PrefixSet) Type() DefinedType {
	return DEFINED_TYPE_PREFIX
}

func (lhs *PrefixSet) Append(arg DefinedSet) error {
	rhs, ok := arg.(*PrefixSet)
	if !ok {
		return fmt.Errorf("type cast failed")
	}

	if rhs.tree.Size() == 0 {
		// if try to append an empty set, then return directly
		return nil
	} else if lhs.tree.Size() != 0 && rhs.family != lhs.family {
		return fmt.Errorf("can't append different family")
	}
	rhs.tree.Walk(nil, func(r *net.IPNet, v interface{}) bool {
		w, ok, _ := lhs.tree.Get(r)
		if ok {
			rp := v.([]*Prefix)
			lp := w.([]*Prefix)
			lhs.tree.Add(r, append(lp, rp...))
		} else {
			lhs.tree.Add(r, v)
		}
		return true
	})
	lhs.family = rhs.family
	return nil
}

func (lhs *PrefixSet) Remove(arg DefinedSet) error {
	rhs, ok := arg.(*PrefixSet)
	if !ok {
		return fmt.Errorf("type cast failed")
	}
	rhs.tree.Walk(nil, func(r *net.IPNet, v interface{}) bool {
		w, ok, _ := lhs.tree.Get(r)
		if !ok {
			return true
		}
		rp := v.([]*Prefix)
		lp := w.([]*Prefix)
		new := make([]*Prefix, 0, len(lp))
		for _, lp := range lp {
			delete := false
			for _, rp := range rp {
				if lp.Equal(rp) {
					delete = true
					break
				}
			}
			if !delete {
				new = append(new, lp)
			}
		}
		if len(new) == 0 {
			lhs.tree.Delete(r)
		} else {
			lhs.tree.Add(r, new)
		}
		return true
	})
	return nil
}

func (lhs *PrefixSet) Replace(arg DefinedSet) error {
	rhs, ok := arg.(*PrefixSet)
	if !ok {
		return fmt.Errorf("type cast failed")
	}
	lhs.tree = rhs.tree
	lhs.family = rhs.family
	return nil
}

func (s *PrefixSet) List() []string {
	var list []string
	s.tree.Walk(nil, func(_ *net.IPNet, v interface{}) bool {
		ps := v.([]*Prefix)
		for _, p := range ps {
			list = append(list, fmt.Sprintf("%s %d..%d", p.PrefixString(), p.MasklengthRangeMin, p.MasklengthRangeMax))
		}
		return true
	})
	return list
}

func (s *PrefixSet) ToConfig() *config.PrefixSet {
	list := make([]config.Prefix, 0, s.tree.Size())
	s.tree.Walk(nil, func(_ *net.IPNet, v interface{}) bool {
		ps := v.([]*Prefix)
		for _, p := range ps {
			list = append(list, config.Prefix{IpPrefix: p.PrefixString(), MasklengthRange: fmt.Sprintf("%d..%d", p.MasklengthRangeMin, p.MasklengthRangeMax)})
		}
		return true
	})
	return &config.PrefixSet{
		PrefixSetName: s.name,
		PrefixList:    list,
	}
}

func (s *PrefixSet) String() string {
	return strings.Join(s.List(), "\n")
}

type NextHopSet struct {
	list []net.IPNet
}

func (s *NextHopSet) Name() string {
	return "NextHopSet: NO NAME"
}

func (s *NextHopSet) Type() DefinedType {
	return DEFINED_TYPE_NEXT_HOP
}

func (lhs *NextHopSet) Append(arg DefinedSet) error {
	rhs, ok := arg.(*NextHopSet)
	if !ok {
		return fmt.Errorf("type cast failed")
	}
	lhs.list = append(lhs.list, rhs.list...)
	return nil
}

func (lhs *NextHopSet) Remove(arg DefinedSet) error {
	rhs, ok := arg.(*NextHopSet)
	if !ok {
		return fmt.Errorf("type cast failed")
	}
	ps := make([]net.IPNet, 0, len(lhs.list))
	for _, x := range lhs.list {
		found := false
		for _, y := range rhs.list {
			if x.String() == y.String() {
				found = true
				break
			}
		}
		if !found {
			ps = append(ps, x)
		}
	}
	lhs.list = ps
	return nil
}

func (lhs *NextHopSet) Replace(arg DefinedSet) error {
	rhs, ok := arg.(*NextHopSet)
	if !ok {
		return fmt.Errorf("type cast failed")
	}
	lhs.list = rhs.list
	return nil
}

func (s *NextHopSet) List() []string {
	list := make([]string, 0, len(s.list))
	for _, n := range s.list {
		list = append(list, n.String())
	}
	return list
}

func (s *NextHopSet) ToConfig() []string {
	return s.List()
}

func (s *NextHopSet) String() string {
	return "[ " + strings.Join(s.List(), ", ") + " ]"
}

type NeighborSet struct {
	name string
	list []net.IPNet
}

func (s *NeighborSet) Name() string {
	return s.name
}

func (s *NeighborSet) Type() DefinedType {
	return DEFINED_TYPE_NEIGHBOR
}

func (lhs *NeighborSet) Append(arg DefinedSet) error {
	rhs, ok := arg.(*NeighborSet)
	if !ok {
		return fmt.Errorf("type cast failed")
	}
	lhs.list = append(lhs.list, rhs.list...)
	return nil
}

func (lhs *NeighborSet) Remove(arg DefinedSet) error {
	rhs, ok := arg.(*NeighborSet)
	if !ok {
		return fmt.Errorf("type cast failed")
	}
	ps := make([]net.IPNet, 0, len(lhs.list))
	for _, x := range lhs.list {
		found := false
		for _, y := range rhs.list {
			if x.String() == y.String() {
				found = true
				break
			}
		}
		if !found {
			ps = append(ps, x)
		}
	}
	lhs.list = ps
	return nil
}

func (lhs *NeighborSet) Replace(arg DefinedSet) error {
	rhs, ok := arg.(*NeighborSet)
	if !ok {
		return fmt.Errorf("type cast failed")
	}
	lhs.list = rhs.list
	return nil
}

func (s *NeighborSet) List() []string {
	list := make([]string, 0, len(s.list))
	for _, n := range s.list {
		list = append(list, n.String())
	}
	return list
}

func (s *NeighborSet) ToConfig() *config.NeighborSet {
	return &config.NeighborSet{
		NeighborSetName:  s.name,
		NeighborInfoList: s.List(),
	}
}

func (s *NeighborSet) String() string {
	return strings.Join(s.List(), "\n")
}

type singleAsPathMatchMode int

const (
	INCLUDE singleAsPathMatchMode = iota
	LEFT_MOST
	ORIGIN
	ONLY
)

type singleAsPathMatch struct {
	asn  uint32
	mode singleAsPathMatchMode
}

func (lhs *singleAsPathMatch) Equal(rhs *singleAsPathMatch) bool {
	return lhs.asn == rhs.asn && lhs.mode == rhs.mode
}

func (lhs *singleAsPathMatch) String() string {
	switch lhs.mode {
	case INCLUDE:
		return fmt.Sprintf("_%d_", lhs.asn)
	case LEFT_MOST:
		return fmt.Sprintf("^%d_", lhs.asn)
	case ORIGIN:
		return fmt.Sprintf("_%d$", lhs.asn)
	case ONLY:
		return fmt.Sprintf("^%d$", lhs.asn)
	}
	return ""
}

func (m *singleAsPathMatch) Match(aspath []uint32) bool {
	if len(aspath) == 0 {
		return false
	}
	switch m.mode {
	case INCLUDE:
		for _, asn := range aspath {
			if m.asn == asn {
				return true
			}
		}
	case LEFT_MOST:
		if m.asn == aspath[0] {
			return true
		}
	case ORIGIN:
		if m.asn == aspath[len(aspath)-1] {
			return true
		}
	case ONLY:
		if len(aspath) == 1 && m.asn == aspath[0] {
			return true
		}
	}
	return false
}

var (
	_regexpLeftMostRe = regexp.MustCompile(`^\^([0-9]+)_$`)
	_regexpOriginRe   = regexp.MustCompile(`^_([0-9]+)\$$`)
	_regexpIncludeRe  = regexp.MustCompile("^_([0-9]+)_$")
	_regexpOnlyRe     = regexp.MustCompile(`^\^([0-9]+)\$$`)
)

type AsPathSet struct {
	typ        DefinedType
	name       string
	list       []*regexp.Regexp
	singleList []*singleAsPathMatch
}

func (s *AsPathSet) Name() string {
	return s.name
}

func (s *AsPathSet) Type() DefinedType {
	return s.typ
}

func (lhs *AsPathSet) Append(arg DefinedSet) error {
	if lhs.Type() != arg.Type() {
		return fmt.Errorf("can't append to different type of defined-set")
	}
	lhs.list = append(lhs.list, arg.(*AsPathSet).list...)
	lhs.singleList = append(lhs.singleList, arg.(*AsPathSet).singleList...)
	return nil
}

func (lhs *AsPathSet) Remove(arg DefinedSet) error {
	if lhs.Type() != arg.Type() {
		return fmt.Errorf("can't append to different type of defined-set")
	}
	newList := make([]*regexp.Regexp, 0, len(lhs.list))
	for _, x := range lhs.list {
		found := false
		for _, y := range arg.(*AsPathSet).list {
			if x.String() == y.String() {
				found = true
				break
			}
		}
		if !found {
			newList = append(newList, x)
		}
	}
	lhs.list = newList
	newSingleList := make([]*singleAsPathMatch, 0, len(lhs.singleList))
	for _, x := range lhs.singleList {
		found := false
		for _, y := range arg.(*AsPathSet).singleList {
			if x.Equal(y) {
				found = true
				break
			}
		}
		if !found {
			newSingleList = append(newSingleList, x)
		}
	}
	lhs.singleList = newSingleList
	return nil
}

func (lhs *AsPathSet) Replace(arg DefinedSet) error {
	rhs, ok := arg.(*AsPathSet)
	if !ok {
		return fmt.Errorf("type cast failed")
	}
	lhs.list = rhs.list
	lhs.singleList = rhs.singleList
	return nil
}

func (s *AsPathSet) List() []string {
	list := make([]string, 0, len(s.list)+len(s.singleList))
	for _, exp := range s.singleList {
		list = append(list, exp.String())
	}
	for _, exp := range s.list {
		list = append(list, exp.String())
	}
	return list
}

func (s *AsPathSet) ToConfig() *config.AsPathSet {
	return &config.AsPathSet{
		AsPathSetName: s.name,
		AsPathList:    s.List(),
	}
}

func (s *AsPathSet) String() string {
	return strings.Join(s.List(), "\n")
}

type regExpSet struct {
	typ  DefinedType
	name string
	list []*regexp.Regexp
}

func (s *regExpSet) Name() string {
	return s.name
}

func (s *regExpSet) Type() DefinedType {
	return s.typ
}

func (lhs *regExpSet) Append(arg DefinedSet) error {
	if lhs.Type() != arg.Type() {
		return fmt.Errorf("can't append to different type of defined-set")
	}
	var list []*regexp.Regexp
	switch lhs.Type() {
	case DEFINED_TYPE_AS_PATH:
		list = arg.(*AsPathSet).list
	case DEFINED_TYPE_COMMUNITY:
		list = arg.(*CommunitySet).list
	case DEFINED_TYPE_EXT_COMMUNITY:
		list = arg.(*ExtCommunitySet).list
	case DEFINED_TYPE_LARGE_COMMUNITY:
		list = arg.(*LargeCommunitySet).list
	default:
		return fmt.Errorf("invalid defined-set type: %d", lhs.Type())
	}
	lhs.list = append(lhs.list, list...)
	return nil
}

func (lhs *regExpSet) Remove(arg DefinedSet) error {
	if lhs.Type() != arg.Type() {
		return fmt.Errorf("can't append to different type of defined-set")
	}
	var list []*regexp.Regexp
	switch lhs.Type() {
	case DEFINED_TYPE_AS_PATH:
		list = arg.(*AsPathSet).list
	case DEFINED_TYPE_COMMUNITY:
		list = arg.(*CommunitySet).list
	case DEFINED_TYPE_EXT_COMMUNITY:
		list = arg.(*ExtCommunitySet).list
	case DEFINED_TYPE_LARGE_COMMUNITY:
		list = arg.(*LargeCommunitySet).list
	default:
		return fmt.Errorf("invalid defined-set type: %d", lhs.Type())
	}
	ps := make([]*regexp.Regexp, 0, len(lhs.list))
	for _, x := range lhs.list {
		found := false
		for _, y := range list {
			if x.String() == y.String() {
				found = true
				break
			}
		}
		if !found {
			ps = append(ps, x)
		}
	}
	lhs.list = ps
	return nil
}

func (lhs *regExpSet) Replace(arg DefinedSet) error {
	switch c := arg.(type) {
	case *CommunitySet:
		lhs.list = c.list
	case *ExtCommunitySet:
		lhs.list = c.list
	case *LargeCommunitySet:
		lhs.list = c.list
	default:
		return fmt.Errorf("type cast failed")
	}
	return nil
}

type CommunitySet struct {
	regExpSet
}

func (s *CommunitySet) List() []string {
	list := make([]string, 0, len(s.list))
	for _, exp := range s.list {
		list = append(list, exp.String())
	}
	return list
}

func (s *CommunitySet) ToConfig() *config.CommunitySet {
	return &config.CommunitySet{
		CommunitySetName: s.name,
		CommunityList:    s.List(),
	}
}

func (s *CommunitySet) String() string {
	return strings.Join(s.List(), "\n")
}

var _regexpCommunity = regexp.MustCompile(`(\d+):(\d+)`)

func ParseCommunity(arg string) (uint32, error) {
	i, err := strconv.ParseUint(arg, 10, 32)
	if err == nil {
		return uint32(i), nil
	}

	elems := _regexpCommunity.FindStringSubmatch(arg)
	if len(elems) == 3 {
		fst, _ := strconv.ParseUint(elems[1], 10, 16)
		snd, _ := strconv.ParseUint(elems[2], 10, 16)
		return uint32(fst<<16 | snd), nil
	}
	for i, v := range bgp.WellKnownCommunityNameMap {
		if arg == v {
			return uint32(i), nil
		}
	}
	return 0, fmt.Errorf("failed to parse %s as community", arg)
}

func ParseExtCommunity(arg string) (bgp.ExtendedCommunityInterface, error) {
	var subtype bgp.ExtendedCommunityAttrSubType
	var value string
	elems := strings.SplitN(arg, ":", 2)

	isValidationState := func(s string) bool {
		s = strings.ToLower(s)
		r := s == bgp.VALIDATION_STATE_VALID.String()
		r = r || s == bgp.VALIDATION_STATE_NOT_FOUND.String()
		return r || s == bgp.VALIDATION_STATE_INVALID.String()
	}
	if len(elems) < 2 && (len(elems) < 1 && !isValidationState(elems[0])) {
		return nil, fmt.Errorf("invalid ext-community (rt|soo):<value> | valid | not-found | invalid")
	}
	if isValidationState(elems[0]) {
		subtype = bgp.EC_SUBTYPE_ORIGIN_VALIDATION
		value = elems[0]
	} else {
		switch strings.ToLower(elems[0]) {
		case "rt":
			subtype = bgp.EC_SUBTYPE_ROUTE_TARGET
		case "soo":
			subtype = bgp.EC_SUBTYPE_ROUTE_ORIGIN
		default:
			return nil, fmt.Errorf("invalid ext-community (rt|soo):<value> | valid | not-found | invalid")
		}
		value = elems[1]
	}
	return bgp.ParseExtendedCommunity(subtype, value)
}

var _regexpCommunity2 = regexp.MustCompile(`^(\d+.)*\d+:\d+$`)

func ParseCommunityRegexp(arg string) (*regexp.Regexp, error) {
	i, err := strconv.ParseUint(arg, 10, 32)
	if err == nil {
		return regexp.Compile(fmt.Sprintf("^%d:%d$", i>>16, i&0x0000ffff))
	}

	if _regexpCommunity2.MatchString(arg) {
		return regexp.Compile(fmt.Sprintf("^%s$", arg))
	}

	for i, v := range bgp.WellKnownCommunityNameMap {
		if strings.Replace(strings.ToLower(arg), "_", "-", -1) == v {
			return regexp.Compile(fmt.Sprintf("^%d:%d$", i>>16, i&0x0000ffff))
		}
	}

	return regexp.Compile(arg)
}

type ExtCommunitySet struct {
	regExpSet
	subtypeList []bgp.ExtendedCommunityAttrSubType
}

func (s *ExtCommunitySet) List() []string {
	list := make([]string, 0, len(s.list))
	f := func(idx int, arg string) string {
		switch s.subtypeList[idx] {
		case bgp.EC_SUBTYPE_ROUTE_TARGET:
			return fmt.Sprintf("rt:%s", arg)
		case bgp.EC_SUBTYPE_ROUTE_ORIGIN:
			return fmt.Sprintf("soo:%s", arg)
		case bgp.EC_SUBTYPE_ORIGIN_VALIDATION:
			return arg
		default:
			return fmt.Sprintf("%d:%s", s.subtypeList[idx], arg)
		}
	}
	for idx, exp := range s.list {
		list = append(list, f(idx, exp.String()))
	}
	return list
}

func (s *ExtCommunitySet) ToConfig() *config.ExtCommunitySet {
	return &config.ExtCommunitySet{
		ExtCommunitySetName: s.name,
		ExtCommunityList:    s.List(),
	}
}

func (s *ExtCommunitySet) String() string {
	return strings.Join(s.List(), "\n")
}

func (s *ExtCommunitySet) Append(arg DefinedSet) error {
	err := s.regExpSet.Append(arg)
	if err != nil {
		return err
	}
	sList := arg.(*ExtCommunitySet).subtypeList
	s.subtypeList = append(s.subtypeList, sList...)
	return nil
}

type LargeCommunitySet struct {
	regExpSet
}

func (s *LargeCommunitySet) List() []string {
	list := make([]string, 0, len(s.list))
	for _, exp := range s.list {
		list = append(list, exp.String())
	}
	return list
}

func (s *LargeCommunitySet) ToConfig() *config.LargeCommunitySet {
	return &config.LargeCommunitySet{
		LargeCommunitySetName: s.name,
		LargeCommunityList:    s.List(),
	}
}

func (s *LargeCommunitySet) String() string {
	return strings.Join(s.List(), "\n")
}

var _regexpCommunityLarge = regexp.MustCompile(`\d+:\d+:\d+`)

func ParseLargeCommunityRegexp(arg string) (*regexp.Regexp, error) {
	if _regexpCommunityLarge.MatchString(arg) {
		return regexp.Compile(fmt.Sprintf("^%s$", arg))
	}
	exp, err := regexp.Compile(arg)
	if err != nil {
		return nil, fmt.Errorf("invalid large-community format: %v", err)
	}

	return exp, nil
}

type Condition interface {
	Name() string
	Type() ConditionType
	Evaluate(*Path, *PolicyOptions) bool
	Set() DefinedSet
}

type NextHopCondition struct {
	set *NextHopSet
}

func (c *NextHopCondition) Type() ConditionType {
	return CONDITION_NEXT_HOP
}

func (c *NextHopCondition) Set() DefinedSet {
	return c.set
}

func (c *NextHopCondition) Name() string { return "" }

func (c *NextHopCondition) String() string {
	return c.set.String()
}

// compare next-hop ipaddress of this condition and source address of path
// and, subsequent comparisons are skipped if that matches the conditions.
// If NextHopSet's length is zero, return true.
func (c *NextHopCondition) Evaluate(path *Path, options *PolicyOptions) bool {
	if len(c.set.list) == 0 {
		log.WithFields(log.Fields{
			"Topic": "Policy",
		}).Debug("NextHop doesn't have elements")
		return true
	}

	nexthop := path.GetNexthop()

	// In cases where we advertise routes from iBGP to eBGP, we want to filter
	// on the "original" nexthop. The current paths' nexthop has already been
	// set and is ready to be advertised as per:
	// https://tools.ietf.org/html/rfc4271#section-5.1.3
	if options != nil && options.OldNextHop != nil &&
		!options.OldNextHop.IsUnspecified() && !options.OldNextHop.Equal(nexthop) {
		nexthop = options.OldNextHop
	}

	if nexthop == nil {
		return false
	}

	for _, n := range c.set.list {
		if n.Contains(nexthop) {
			return true
		}
	}

	return false
}

type PrefixCondition struct {
	set    *PrefixSet
	option MatchOption
}

func (c *PrefixCondition) Type() ConditionType {
	return CONDITION_PREFIX
}

func (c *PrefixCondition) Set() DefinedSet {
	return c.set
}

func (c *PrefixCondition) Option() MatchOption {
	return c.option
}

// compare prefixes in this condition and nlri of path and
// subsequent comparison is skipped if that matches the conditions.
// If PrefixList's length is zero, return true.
func (c *PrefixCondition) Evaluate(path *Path, _ *PolicyOptions) bool {
	if path.GetRouteFamily() != c.set.family {
		return false
	}

	r := nlriToIPNet(path.GetNlri())
	ones, _ := r.Mask.Size()
	masklen := uint8(ones)
	result := false
	if _, ps, _ := c.set.tree.Match(r); ps != nil {
		for _, p := range ps.([]*Prefix) {
			if p.MasklengthRangeMin <= masklen && masklen <= p.MasklengthRangeMax {
				result = true
				break
			}
		}
	}

	if c.option == MATCH_OPTION_INVERT {
		result = !result
	}

	return result
}

func (c *PrefixCondition) Name() string { return c.set.name }

type NeighborCondition struct {
	set    *NeighborSet
	option MatchOption
}

func (c *NeighborCondition) Type() ConditionType {
	return CONDITION_NEIGHBOR
}

func (c *NeighborCondition) Set() DefinedSet {
	return c.set
}

func (c *NeighborCondition) Option() MatchOption {
	return c.option
}

// compare neighbor ipaddress of this condition and source address of path
// and, subsequent comparisons are skipped if that matches the conditions.
// If NeighborList's length is zero, return true.
func (c *NeighborCondition) Evaluate(path *Path, options *PolicyOptions) bool {
	if len(c.set.list) == 0 {
		log.WithFields(log.Fields{
			"Topic": "Policy",
		}).Debug("NeighborList doesn't have elements")
		return true
	}

	neighbor := path.GetSource().Address
	if options != nil && options.Info != nil && options.Info.Address != nil {
		neighbor = options.Info.Address
	}

	if neighbor == nil {
		return false
	}
	result := false
	for _, n := range c.set.list {
		if n.Contains(neighbor) {
			result = true
			break
		}
	}

	if c.option == MATCH_OPTION_INVERT {
		result = !result
	}

	return result
}

func (c *NeighborCondition) Name() string { return c.set.name }

type AsPathCondition struct {
	set    *AsPathSet
	option MatchOption
}

func (c *AsPathCondition) Type() ConditionType {
	return CONDITION_AS_PATH
}

func (c *AsPathCondition) Set() DefinedSet {
	return c.set
}

func (c *AsPathCondition) Option() MatchOption {
	return c.option
}

func (c *AsPathCondition) Evaluate(path *Path, _ *PolicyOptions) bool {
	if len(c.set.singleList) > 0 {
		aspath := path.GetAsSeqList()
		for _, m := range c.set.singleList {
			result := m.Match(aspath)
			if c.option == MATCH_OPTION_ALL && !result {
				return false
			}
			if c.option == MATCH_OPTION_ANY && result {
				return true
			}
			if c.option == MATCH_OPTION_INVERT && result {
				return false
			}
		}
	}
	if len(c.set.list) > 0 {
		aspath := path.GetAsString()
		for _, r := range c.set.list {
			result := r.MatchString(aspath)
			if c.option == MATCH_OPTION_ALL && !result {
				return false
			}
			if c.option == MATCH_OPTION_ANY && result {
				return true
			}
			if c.option == MATCH_OPTION_INVERT && result {
				return false
			}
		}
	}
	if c.option == MATCH_OPTION_ANY {
		return false
	}
	return true
}

func (c *AsPathCondition) Name() string { return c.set.name }

type CommunityCondition struct {
	set    *CommunitySet
	option MatchOption
}

func (c *CommunityCondition) Type() ConditionType {
	return CONDITION_COMMUNITY
}

func (c *CommunityCondition) Set() DefinedSet {
	return c.set
}

func (c *CommunityCondition) Option() MatchOption {
	return c.option
}

func (c *CommunityCondition) Evaluate(path *Path, _ *PolicyOptions) bool {
	cs := path.GetCommunities()
	result := false
	for _, x := range c.set.list {
		result = false
		for _, y := range cs {
			if x.MatchString(fmt.Sprintf("%d:%d", y>>16, y&0x0000ffff)) {
				result = true
				break
			}
		}
		if c.option == MATCH_OPTION_ALL && !result {
			break
		}
		if (c.option == MATCH_OPTION_ANY || c.option == MATCH_OPTION_INVERT) && result {
			break
		}
	}
	if c.option == MATCH_OPTION_INVERT {
		result = !result
	}
	return result
}

func (c *CommunityCondition) Name() string { return c.set.name }

type ExtCommunityCondition struct {
	set    *ExtCommunitySet
	option MatchOption
}

func (c *ExtCommunityCondition) Type() ConditionType {
	return CONDITION_EXT_COMMUNITY
}

func (c *ExtCommunityCondition) Set() DefinedSet {
	return c.set
}

func (c *ExtCommunityCondition) Option() MatchOption {
	return c.option
}

func (c *ExtCommunityCondition) Evaluate(path *Path, _ *PolicyOptions) bool {
	es := path.GetExtCommunities()
	result := false
	for _, x := range es {
		result = false
		typ, subtype := x.GetTypes()
		// match only with transitive community. see RFC7153
		if typ >= 0x3f {
			continue
		}
		for idx, y := range c.set.list {
			if subtype == c.set.subtypeList[idx] && y.MatchString(x.String()) {
				result = true
				break
			}
		}
		if c.option == MATCH_OPTION_ALL && !result {
			break
		}
		if c.option == MATCH_OPTION_ANY && result {
			break
		}
	}
	if c.option == MATCH_OPTION_INVERT {
		result = !result
	}
	return result
}

func (c *ExtCommunityCondition) Name() string { return c.set.name }

type LargeCommunityCondition struct {
	set    *LargeCommunitySet
	option MatchOption
}

func (c *LargeCommunityCondition) Type() ConditionType {
	return CONDITION_LARGE_COMMUNITY
}

func (c *LargeCommunityCondition) Set() DefinedSet {
	return c.set
}

func (c *LargeCommunityCondition) Option() MatchOption {
	return c.option
}

func (c *LargeCommunityCondition) Evaluate(path *Path, _ *PolicyOptions) bool {
	result := false
	cs := path.GetLargeCommunities()
	for _, x := range c.set.list {
		result = false
		for _, y := range cs {
			if x.MatchString(y.String()) {
				result = true
				break
			}
		}
		if c.option == MATCH_OPTION_ALL && !result {
			break
		}
		if (c.option == MATCH_OPTION_ANY || c.option == MATCH_OPTION_INVERT) && result {
			break
		}
	}
	if c.option == MATCH_OPTION_INVERT {
		result = !result
	}
	return result
}

func (c *LargeCommunityCondition) Name() string { return c.set.name }

type AsPathLengthCondition struct {
	length   uint32
	operator AttributeComparison
}

func (c *AsPathLengthCondition) Type() ConditionType {
	return CONDITION_AS_PATH_LENGTH
}

// compare AS_PATH length in the message's AS_PATH attribute with
// the one in condition.
func (c *AsPathLengthCondition) Evaluate(path *Path, _ *PolicyOptions) bool {

	length := uint32(path.GetAsPathLen())
	result := false
	switch c.operator {
	case ATTRIBUTE_EQ:
		result = c.length == length
	case ATTRIBUTE_GE:
		result = c.length <= length
	case ATTRIBUTE_LE:
		result = c.length >= length
	}

	return result
}

func (c *AsPathLengthCondition) Set() DefinedSet {
	return nil
}

func (c *AsPathLengthCondition) Name() string { return "" }

func (c *AsPathLengthCondition) String() string {
	return fmt.Sprintf("%s%d", c.operator, c.length)
}

type RouteTypeCondition struct {
	typ config.RouteType
}

func (c *RouteTypeCondition) Type() ConditionType {
	return CONDITION_ROUTE_TYPE
}

func (c *RouteTypeCondition) Evaluate(path *Path, _ *PolicyOptions) bool {
	switch c.typ {
	case config.ROUTE_TYPE_LOCAL:
		return path.IsLocal()
	case config.ROUTE_TYPE_INTERNAL:
		return !path.IsLocal() && path.IsIBGP()
	case config.ROUTE_TYPE_EXTERNAL:
		return !path.IsLocal() && !path.IsIBGP()
	}
	return false
}

func (c *RouteTypeCondition) Set() DefinedSet {
	return nil
}

func (c *RouteTypeCondition) Name() string { return "" }

func (c *RouteTypeCondition) String() string {
	return string(c.typ)
}

type AfiSafiInCondition struct {
	routeFamilies []bgp.RouteFamily
}

func (c *AfiSafiInCondition) Type() ConditionType {
	return CONDITION_AFI_SAFI_IN
}

func (c *AfiSafiInCondition) Evaluate(path *Path, _ *PolicyOptions) bool {
	for _, rf := range c.routeFamilies {
		if path.GetRouteFamily() == rf {
			return true
		}
	}
	return false
}

func (c *AfiSafiInCondition) Set() DefinedSet {
	return nil
}

func (c *AfiSafiInCondition) Name() string { return "" }

func (c *AfiSafiInCondition) String() string {
	tmp := make([]string, 0, len(c.routeFamilies))
	for _, afiSafi := range c.routeFamilies {
		tmp = append(tmp, afiSafi.String())
	}
	return strings.Join(tmp, " ")
}

type Action interface {
	Type() ActionType
	Apply(*Path, *PolicyOptions) *Path
	String() string
}

type RoutingAction struct {
	AcceptRoute bool
}

func (a *RoutingAction) Type() ActionType {
	return ACTION_ROUTING
}

func (a *RoutingAction) Apply(path *Path, _ *PolicyOptions) *Path {
	if a.AcceptRoute {
		return path
	}
	return nil
}

func (a *RoutingAction) String() string {
	action := "reject"
	if a.AcceptRoute {
		action = "accept"
	}
	return action
}

type CommunityAction struct {
	action     config.BgpSetCommunityOptionType
	list       []uint32
	removeList []*regexp.Regexp
}

func RegexpRemoveCommunities(path *Path, exps []*regexp.Regexp) {
	comms := path.GetCommunities()
	newComms := make([]uint32, 0, len(comms))
	for _, comm := range comms {
		c := fmt.Sprintf("%d:%d", comm>>16, comm&0x0000ffff)
		match := false
		for _, exp := range exps {
			if exp.MatchString(c) {
				match = true
				break
			}
		}
		if !match {
			newComms = append(newComms, comm)
		}
	}
	path.SetCommunities(newComms, true)
}

func RegexpRemoveExtCommunities(path *Path, exps []*regexp.Regexp, subtypes []bgp.ExtendedCommunityAttrSubType) {
	comms := path.GetExtCommunities()
	newComms := make([]bgp.ExtendedCommunityInterface, 0, len(comms))
	for _, comm := range comms {
		match := false
		typ, subtype := comm.GetTypes()
		// match only with transitive community. see RFC7153
		if typ >= 0x3f {
			continue
		}
		for idx, exp := range exps {
			if subtype == subtypes[idx] && exp.MatchString(comm.String()) {
				match = true
				break
			}
		}
		if !match {
			newComms = append(newComms, comm)
		}
	}
	path.SetExtCommunities(newComms, true)
}

func RegexpRemoveLargeCommunities(path *Path, exps []*regexp.Regexp) {
	comms := path.GetLargeCommunities()
	newComms := make([]*bgp.LargeCommunity, 0, len(comms))
	for _, comm := range comms {
		c := comm.String()
		match := false
		for _, exp := range exps {
			if exp.MatchString(c) {
				match = true
				break
			}
		}
		if !match {
			newComms = append(newComms, comm)
		}
	}
	path.SetLargeCommunities(newComms, true)
}

func (a *CommunityAction) Type() ActionType {
	return ACTION_COMMUNITY
}

func (a *CommunityAction) Apply(path *Path, _ *PolicyOptions) *Path {
	switch a.action {
	case config.BGP_SET_COMMUNITY_OPTION_TYPE_ADD:
		path.SetCommunities(a.list, false)
	case config.BGP_SET_COMMUNITY_OPTION_TYPE_REMOVE:
		RegexpRemoveCommunities(path, a.removeList)
	case config.BGP_SET_COMMUNITY_OPTION_TYPE_REPLACE:
		path.SetCommunities(a.list, true)
	}
	return path
}

func (a *CommunityAction) ToConfig() *config.SetCommunity {
	cs := make([]string, 0, len(a.list)+len(a.removeList))
	for _, comm := range a.list {
		c := fmt.Sprintf("%d:%d", comm>>16, comm&0x0000ffff)
		cs = append(cs, c)
	}
	for _, exp := range a.removeList {
		cs = append(cs, exp.String())
	}
	return &config.SetCommunity{
		Options:            string(a.action),
		SetCommunityMethod: config.SetCommunityMethod{CommunitiesList: cs},
	}
}

// TODO: this is not efficient use of regexp, probably slow
var _regexpCommunityReplaceString = regexp.MustCompile(`[\^\$]`)

func (a *CommunityAction) String() string {
	list := a.ToConfig().SetCommunityMethod.CommunitiesList
	l := _regexpCommunityReplaceString.ReplaceAllString(strings.Join(list, ", "), "")
	return fmt.Sprintf("%s[%s]", a.action, l)
}

type ExtCommunityAction struct {
	action      config.BgpSetCommunityOptionType
	list        []bgp.ExtendedCommunityInterface
	removeList  []*regexp.Regexp
	subtypeList []bgp.ExtendedCommunityAttrSubType
}

func (a *ExtCommunityAction) Type() ActionType {
	return ACTION_EXT_COMMUNITY
}

func (a *ExtCommunityAction) Apply(path *Path, _ *PolicyOptions) *Path {
	switch a.action {
	case config.BGP_SET_COMMUNITY_OPTION_TYPE_ADD:
		path.SetExtCommunities(a.list, false)
	case config.BGP_SET_COMMUNITY_OPTION_TYPE_REMOVE:
		RegexpRemoveExtCommunities(path, a.removeList, a.subtypeList)
	case config.BGP_SET_COMMUNITY_OPTION_TYPE_REPLACE:
		path.SetExtCommunities(a.list, true)
	}
	return path
}

func (a *ExtCommunityAction) ToConfig() *config.SetExtCommunity {
	cs := make([]string, 0, len(a.list)+len(a.removeList))
	f := func(idx int, arg string) string {
		switch a.subtypeList[idx] {
		case bgp.EC_SUBTYPE_ROUTE_TARGET:
			return fmt.Sprintf("rt:%s", arg)
		case bgp.EC_SUBTYPE_ROUTE_ORIGIN:
			return fmt.Sprintf("soo:%s", arg)
		case bgp.EC_SUBTYPE_ORIGIN_VALIDATION:
			return arg
		default:
			return fmt.Sprintf("%d:%s", a.subtypeList[idx], arg)
		}
	}
	for idx, c := range a.list {
		cs = append(cs, f(idx, c.String()))
	}
	for idx, exp := range a.removeList {
		cs = append(cs, f(idx, exp.String()))
	}
	return &config.SetExtCommunity{
		Options: string(a.action),
		SetExtCommunityMethod: config.SetExtCommunityMethod{
			CommunitiesList: cs,
		},
	}
}

func (a *ExtCommunityAction) String() string {
	list := a.ToConfig().SetExtCommunityMethod.CommunitiesList
	l := _regexpCommunityReplaceString.ReplaceAllString(strings.Join(list, ", "), "")
	return fmt.Sprintf("%s[%s]", a.action, l)
}

type LargeCommunityAction struct {
	action     config.BgpSetCommunityOptionType
	list       []*bgp.LargeCommunity
	removeList []*regexp.Regexp
}

func (a *LargeCommunityAction) Type() ActionType {
	return ACTION_LARGE_COMMUNITY
}

func (a *LargeCommunityAction) Apply(path *Path, _ *PolicyOptions) *Path {
	switch a.action {
	case config.BGP_SET_COMMUNITY_OPTION_TYPE_ADD:
		path.SetLargeCommunities(a.list, false)
	case config.BGP_SET_COMMUNITY_OPTION_TYPE_REMOVE:
		RegexpRemoveLargeCommunities(path, a.removeList)
	case config.BGP_SET_COMMUNITY_OPTION_TYPE_REPLACE:
		path.SetLargeCommunities(a.list, true)
	}
	return path
}

func (a *LargeCommunityAction) ToConfig() *config.SetLargeCommunity {
	cs := make([]string, 0, len(a.list)+len(a.removeList))
	for _, comm := range a.list {
		cs = append(cs, comm.String())
	}
	for _, exp := range a.removeList {
		cs = append(cs, exp.String())
	}
	return &config.SetLargeCommunity{
		SetLargeCommunityMethod: config.SetLargeCommunityMethod{CommunitiesList: cs},
		Options:                 config.BgpSetCommunityOptionType(a.action),
	}
}

func (a *LargeCommunityAction) String() string {
	list := a.ToConfig().SetLargeCommunityMethod.CommunitiesList
	l := _regexpCommunityReplaceString.ReplaceAllString(strings.Join(list, ", "), "")
	return fmt.Sprintf("%s[%s]", a.action, l)
}

type MedAction struct {
	value  int64
	action MedActionType
}

func (a *MedAction) Type() ActionType {
	return ACTION_MED
}

func (a *MedAction) Apply(path *Path, _ *PolicyOptions) *Path {
	var err error
	switch a.action {
	case MED_ACTION_MOD:
		err = path.SetMed(a.value, false)
	case MED_ACTION_REPLACE:
		err = path.SetMed(a.value, true)
	}

	if err != nil {
		log.WithFields(log.Fields{
			"Topic": "Policy",
			"Type":  "Med Action",
			"Error": err,
		}).Warn("Could not set Med on path")
	}
	return path
}

func (a *MedAction) ToConfig() config.BgpSetMedType {
	if a.action == MED_ACTION_MOD && a.value > 0 {
		return config.BgpSetMedType(fmt.Sprintf("+%d", a.value))
	}
	return config.BgpSetMedType(fmt.Sprintf("%d", a.value))
}

func (a *MedAction) String() string {
	return string(a.ToConfig())
}

var _regexpParseMedAction = regexp.MustCompile(`^(\+|\-)?(\d+)$`)

type LocalPrefAction struct {
	value uint32
}

func (a *LocalPrefAction) Type() ActionType {
	return ACTION_LOCAL_PREF
}

func (a *LocalPrefAction) Apply(path *Path, _ *PolicyOptions) *Path {
	path.setPathAttr(bgp.NewPathAttributeLocalPref(a.value))
	return path
}

func (a *LocalPrefAction) ToConfig() uint32 {
	return a.value
}

func (a *LocalPrefAction) String() string {
	return fmt.Sprintf("%d", a.value)
}

type AsPathPrependAction struct {
	asn         uint32
	useLeftMost bool
	repeat      uint8
}

func (a *AsPathPrependAction) Type() ActionType {
	return ACTION_AS_PATH_PREPEND
}

func (a *AsPathPrependAction) Apply(path *Path, option *PolicyOptions) *Path {
	var asn uint32
	if a.useLeftMost {
		aspath := path.GetAsSeqList()
		if len(aspath) == 0 {
			log.WithFields(log.Fields{
				"Topic": "Policy",
				"Type":  "AsPathPrepend Action",
			}).Warn("aspath length is zero.")
			return path
		}
		asn = aspath[0]
		if asn == 0 {
			log.WithFields(log.Fields{
				"Topic": "Policy",
				"Type":  "AsPathPrepend Action",
			}).Warn("left-most ASN is not seq")
			return path
		}
	} else {
		asn = a.asn
	}

	confed := option != nil && option.Info != nil && option.Info.Confederation
	path.PrependAsn(asn, a.repeat, confed)

	return path
}

func (a *AsPathPrependAction) ToConfig() *config.SetAsPathPrepend {
	return &config.SetAsPathPrepend{
		RepeatN: uint8(a.repeat),
		As: func() string {
			if a.useLeftMost {
				return "last-as"
			}
			return fmt.Sprintf("%d", a.asn)
		}(),
	}
}

func (a *AsPathPrependAction) String() string {
	c := a.ToConfig()
	return fmt.Sprintf("prepend %s %d times", c.As, c.RepeatN)
}

type NexthopAction struct {
	value net.IP
	self  bool
}

func (a *NexthopAction) Type() ActionType {
	return ACTION_NEXTHOP
}

func (a *NexthopAction) Apply(path *Path, options *PolicyOptions) *Path {
	if a.self {
		if options != nil && options.Info != nil && options.Info.LocalAddress != nil {
			path.SetNexthop(options.Info.LocalAddress)
		}
		return path
	}
	path.SetNexthop(a.value)
	return path
}

func (a *NexthopAction) ToConfig() config.BgpNextHopType {
	if a.self {
		return config.BgpNextHopType("self")
	}
	return config.BgpNextHopType(a.value.String())
}

func (a *NexthopAction) String() string {
	return string(a.ToConfig())
}

type Statement struct {
	Name        string
	Conditions  []Condition
	RouteAction Action
	ModActions  []Action
}

// evaluate each condition in the statement according to MatchSetOptions
func (s *Statement) Evaluate(p *Path, options *PolicyOptions) bool {
	for _, c := range s.Conditions {
		if !c.Evaluate(p, options) {
			return false
		}
	}
	return true
}
func (s *Statement) ToConfig() *config.Statement {
	return &config.Statement{
		Name: s.Name,
		Conditions: func() config.Conditions {
			cond := config.Conditions{}
			for _, c := range s.Conditions {
				switch v := c.(type) {
				case *PrefixCondition:
					cond.MatchPrefixSet = config.MatchPrefixSet{PrefixSet: v.set.Name(), MatchSetOptions: v.option.ConvertToMatchSetOptionsRestrictedType()}
				case *NeighborCondition:
					cond.MatchNeighborSet = config.MatchNeighborSet{NeighborSet: v.set.Name(), MatchSetOptions: v.option.ConvertToMatchSetOptionsRestrictedType()}
				case *AsPathLengthCondition:
					cond.BgpConditions.AsPathLength = config.AsPathLength{Operator: config.IntToAttributeComparisonMap[int(v.operator)], Value: v.length}
				case *AsPathCondition:
					cond.BgpConditions.MatchAsPathSet = config.MatchAsPathSet{AsPathSet: v.set.Name(), MatchSetOptions: config.IntToMatchSetOptionsTypeMap[int(v.option)]}
				case *CommunityCondition:
					cond.BgpConditions.MatchCommunitySet = config.MatchCommunitySet{CommunitySet: v.set.Name(), MatchSetOptions: config.IntToMatchSetOptionsTypeMap[int(v.option)]}
				case *ExtCommunityCondition:
					cond.BgpConditions.MatchExtCommunitySet = config.MatchExtCommunitySet{ExtCommunitySet: v.set.Name(), MatchSetOptions: config.IntToMatchSetOptionsTypeMap[int(v.option)]}
				case *LargeCommunityCondition:
					cond.BgpConditions.MatchLargeCommunitySet = config.MatchLargeCommunitySet{LargeCommunitySet: v.set.Name(), MatchSetOptions: config.IntToMatchSetOptionsTypeMap[int(v.option)]}
				case *NextHopCondition:
					cond.BgpConditions.NextHopInList = v.set.List()
				case *RouteTypeCondition:
					cond.BgpConditions.RouteType = v.typ
				case *AfiSafiInCondition:
					res := make([]config.AfiSafiType, 0, len(v.routeFamilies))
					for _, rf := range v.routeFamilies {
						res = append(res, config.AfiSafiType(rf.String()))
					}
					cond.BgpConditions.AfiSafiInList = res
				}
			}
			return cond
		}(),
		Actions: func() config.Actions {
			act := config.Actions{}
			if s.RouteAction != nil && !reflect.ValueOf(s.RouteAction).IsNil() {
				a := s.RouteAction.(*RoutingAction)
				if a.AcceptRoute {
					act.RouteDisposition = config.ROUTE_DISPOSITION_ACCEPT_ROUTE
				} else {
					act.RouteDisposition = config.ROUTE_DISPOSITION_REJECT_ROUTE
				}
			} else {
				act.RouteDisposition = config.ROUTE_DISPOSITION_NONE
			}
			for _, a := range s.ModActions {
				switch v := a.(type) {
				case *AsPathPrependAction:
					act.BgpActions.SetAsPathPrepend = *v.ToConfig()
				case *CommunityAction:
					act.BgpActions.SetCommunity = *v.ToConfig()
				case *ExtCommunityAction:
					act.BgpActions.SetExtCommunity = *v.ToConfig()
				case *LargeCommunityAction:
					act.BgpActions.SetLargeCommunity = *v.ToConfig()
				case *MedAction:
					act.BgpActions.SetMed = v.ToConfig()
				case *LocalPrefAction:
					act.BgpActions.SetLocalPref = v.ToConfig()
				case *NexthopAction:
					act.BgpActions.SetNextHop = v.ToConfig()
				}
			}
			return act
		}(),
	}
}

type opType int

const (
	ADD opType = iota
	REMOVE
	REPLACE
)

type Policy struct {
	Name       string
	Statements []*Statement
}

func (p *Policy) ToConfig() *config.PolicyDefinition {
	ss := make([]config.Statement, 0, len(p.Statements))
	for _, s := range p.Statements {
		ss = append(ss, *s.ToConfig())
	}
	return &config.PolicyDefinition{
		Name:       p.Name,
		Statements: ss,
	}
}

func (p *Policy) FillUp(m map[string]*Statement) error {
	stmts := make([]*Statement, 0, len(p.Statements))
	for _, x := range p.Statements {
		y, ok := m[x.Name]
		if !ok {
			return fmt.Errorf("not found statement %s", x.Name)
		}
		stmts = append(stmts, y)
	}
	p.Statements = stmts
	return nil
}

func (lhs *Policy) Add(rhs *Policy) error {
	lhs.Statements = append(lhs.Statements, rhs.Statements...)
	return nil
}

func (lhs *Policy) Remove(rhs *Policy) error {
	stmts := make([]*Statement, 0, len(lhs.Statements))
	for _, x := range lhs.Statements {
		found := false
		for _, y := range rhs.Statements {
			if x.Name == y.Name {
				found = true
				break
			}
		}
		if !found {
			stmts = append(stmts, x)
		}
	}
	lhs.Statements = stmts
	return nil
}

func (lhs *Policy) Replace(rhs *Policy) error {
	lhs.Statements = rhs.Statements
	return nil
}

type Policies []*Policy

func (p Policies) Len() int {
	return len(p)
}

func (p Policies) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

func (p Policies) Less(i, j int) bool {
	return p[i].Name < p[j].Name
}

type PolicyAssignment struct {
	Name     string
	Type     PolicyDirection
	Policies []*Policy
	Default  RouteType
}

var _regexpMedActionType = regexp.MustCompile(`([+-]?)(\d+)`)

func toStatementApi(s *config.Statement) *api.Statement {
	cs := &api.Conditions{}
	o, _ := NewMatchOption(s.Conditions.MatchPrefixSet.MatchSetOptions)
	if s.Conditions.MatchPrefixSet.PrefixSet != "" {
		cs.PrefixSet = &api.MatchSet{
			MatchType: api.MatchType(o),
			Name:      s.Conditions.MatchPrefixSet.PrefixSet,
		}
	}
	if s.Conditions.MatchNeighborSet.NeighborSet != "" {
		o, _ := NewMatchOption(s.Conditions.MatchNeighborSet.MatchSetOptions)
		cs.NeighborSet = &api.MatchSet{
			MatchType: api.MatchType(o),
			Name:      s.Conditions.MatchNeighborSet.NeighborSet,
		}
	}
	if s.Conditions.BgpConditions.AsPathLength.Operator != "" {
		cs.AsPathLength = &api.AsPathLength{
			Length:     s.Conditions.BgpConditions.AsPathLength.Value,
			LengthType: api.AsPathLengthType(s.Conditions.BgpConditions.AsPathLength.Operator.ToInt()),
		}
	}
	if s.Conditions.BgpConditions.MatchAsPathSet.AsPathSet != "" {
		cs.AsPathSet = &api.MatchSet{
			MatchType: api.MatchType(s.Conditions.BgpConditions.MatchAsPathSet.MatchSetOptions.ToInt()),
			Name:      s.Conditions.BgpConditions.MatchAsPathSet.AsPathSet,
		}
	}
	if s.Conditions.BgpConditions.MatchCommunitySet.CommunitySet != "" {
		cs.CommunitySet = &api.MatchSet{
			MatchType: api.MatchType(s.Conditions.BgpConditions.MatchCommunitySet.MatchSetOptions.ToInt()),
			Name:      s.Conditions.BgpConditions.MatchCommunitySet.CommunitySet,
		}
	}
	if s.Conditions.BgpConditions.MatchExtCommunitySet.ExtCommunitySet != "" {
		cs.ExtCommunitySet = &api.MatchSet{
			MatchType: api.MatchType(s.Conditions.BgpConditions.MatchExtCommunitySet.MatchSetOptions.ToInt()),
			Name:      s.Conditions.BgpConditions.MatchExtCommunitySet.ExtCommunitySet,
		}
	}
	if s.Conditions.BgpConditions.MatchLargeCommunitySet.LargeCommunitySet != "" {
		cs.LargeCommunitySet = &api.MatchSet{
			MatchType: api.MatchType(s.Conditions.BgpConditions.MatchLargeCommunitySet.MatchSetOptions.ToInt()),
			Name:      s.Conditions.BgpConditions.MatchLargeCommunitySet.LargeCommunitySet,
		}
	}
	if s.Conditions.BgpConditions.RouteType != "" {
		cs.RouteType = api.Conditions_RouteType(s.Conditions.BgpConditions.RouteType.ToInt())
	}
	if len(s.Conditions.BgpConditions.NextHopInList) > 0 {
		cs.NextHopInList = s.Conditions.BgpConditions.NextHopInList
	}
	if s.Conditions.BgpConditions.AfiSafiInList != nil {
		afiSafiIn := make([]*api.Family, 0)
		for _, afiSafiType := range s.Conditions.BgpConditions.AfiSafiInList {
			if mapped, ok := bgp.AddressFamilyValueMap[string(afiSafiType)]; ok {
				afi, safi := bgp.RouteFamilyToAfiSafi(mapped)
				afiSafiIn = append(afiSafiIn, &api.Family{Afi: api.Family_Afi(afi), Safi: api.Family_Safi(safi)})
			}
		}
		cs.AfiSafiIn = afiSafiIn
	}
	cs.RpkiResult = int32(s.Conditions.BgpConditions.RpkiValidationResult.ToInt())
	as := &api.Actions{
		RouteAction: func() api.RouteAction {
			switch s.Actions.RouteDisposition {
			case config.ROUTE_DISPOSITION_ACCEPT_ROUTE:
				return api.RouteAction_ACCEPT
			case config.ROUTE_DISPOSITION_REJECT_ROUTE:
				return api.RouteAction_REJECT
			}
			return api.RouteAction_NONE
		}(),
		Community: func() *api.CommunityAction {
			if len(s.Actions.BgpActions.SetCommunity.SetCommunityMethod.CommunitiesList) == 0 {
				return nil
			}
			return &api.CommunityAction{
				ActionType:  api.CommunityActionType(config.BgpSetCommunityOptionTypeToIntMap[config.BgpSetCommunityOptionType(s.Actions.BgpActions.SetCommunity.Options)]),
				Communities: s.Actions.BgpActions.SetCommunity.SetCommunityMethod.CommunitiesList}
		}(),
		Med: func() *api.MedAction {
			medStr := strings.TrimSpace(string(s.Actions.BgpActions.SetMed))
			if len(medStr) == 0 {
				return nil
			}
			matches := _regexpMedActionType.FindStringSubmatch(medStr)
			if len(matches) == 0 {
				return nil
			}
			action := api.MedActionType_MED_REPLACE
			switch matches[1] {
			case "+", "-":
				action = api.MedActionType_MED_MOD
			}
			value, err := strconv.ParseInt(matches[1]+matches[2], 10, 64)
			if err != nil {
				return nil
			}
			return &api.MedAction{
				Value:      value,
				ActionType: action,
			}
		}(),
		AsPrepend: func() *api.AsPrependAction {
			if len(s.Actions.BgpActions.SetAsPathPrepend.As) == 0 {
				return nil
			}
			var asn uint64
			useleft := false
			if s.Actions.BgpActions.SetAsPathPrepend.As != "last-as" {
				asn, _ = strconv.ParseUint(s.Actions.BgpActions.SetAsPathPrepend.As, 10, 32)
			} else {
				useleft = true
			}
			return &api.AsPrependAction{
				Asn:         uint32(asn),
				Repeat:      uint32(s.Actions.BgpActions.SetAsPathPrepend.RepeatN),
				UseLeftMost: useleft,
			}
		}(),
		ExtCommunity: func() *api.CommunityAction {
			if len(s.Actions.BgpActions.SetExtCommunity.SetExtCommunityMethod.CommunitiesList) == 0 {
				return nil
			}
			return &api.CommunityAction{
				ActionType:  api.CommunityActionType(config.BgpSetCommunityOptionTypeToIntMap[config.BgpSetCommunityOptionType(s.Actions.BgpActions.SetExtCommunity.Options)]),
				Communities: s.Actions.BgpActions.SetExtCommunity.SetExtCommunityMethod.CommunitiesList,
			}
		}(),
		LargeCommunity: func() *api.CommunityAction {
			if len(s.Actions.BgpActions.SetLargeCommunity.SetLargeCommunityMethod.CommunitiesList) == 0 {
				return nil
			}
			return &api.CommunityAction{
				ActionType:  api.CommunityActionType(config.BgpSetCommunityOptionTypeToIntMap[config.BgpSetCommunityOptionType(s.Actions.BgpActions.SetLargeCommunity.Options)]),
				Communities: s.Actions.BgpActions.SetLargeCommunity.SetLargeCommunityMethod.CommunitiesList,
			}
		}(),
		Nexthop: func() *api.NexthopAction {
			if len(string(s.Actions.BgpActions.SetNextHop)) == 0 {
				return nil
			}

			if string(s.Actions.BgpActions.SetNextHop) == "self" {
				return &api.NexthopAction{
					Self: true,
				}
			}
			return &api.NexthopAction{
				Address: string(s.Actions.BgpActions.SetNextHop),
			}
		}(),
		LocalPref: func() *api.LocalPrefAction {
			if s.Actions.BgpActions.SetLocalPref == 0 {
				return nil
			}
			return &api.LocalPrefAction{Value: s.Actions.BgpActions.SetLocalPref}
		}(),
	}
	return &api.Statement{
		Name:       s.Name,
		Conditions: cs,
		Actions:    as,
	}
}

func NewAPIPolicyFromTableStruct(p *Policy) *api.Policy {
	return ToPolicyApi(p.ToConfig())
}

func ToPolicyApi(p *config.PolicyDefinition) *api.Policy {
	return &api.Policy{
		Name: p.Name,
		Statements: func() []*api.Statement {
			l := make([]*api.Statement, 0)
			for _, s := range p.Statements {
				l = append(l, toStatementApi(&s))
			}
			return l
		}(),
	}
}

func NewAPIPolicyAssignmentFromTableStruct(t *PolicyAssignment) *api.PolicyAssignment {
	return &api.PolicyAssignment{
		Direction: func() api.PolicyDirection {
			switch t.Type {
			case POLICY_DIRECTION_IMPORT:
				return api.PolicyDirection_IMPORT
			case POLICY_DIRECTION_EXPORT:
				return api.PolicyDirection_EXPORT
			}
			log.Errorf("invalid policy-type: %s", t.Type)
			return api.PolicyDirection_UNKNOWN
		}(),
		DefaultAction: func() api.RouteAction {
			switch t.Default {
			case ROUTE_TYPE_ACCEPT:
				return api.RouteAction_ACCEPT
			case ROUTE_TYPE_REJECT:
				return api.RouteAction_REJECT
			}
			return api.RouteAction_NONE
		}(),
		Name: t.Name,
		Policies: func() []*api.Policy {
			l := make([]*api.Policy, 0)
			for _, p := range t.Policies {
				l = append(l, NewAPIPolicyFromTableStruct(p))
			}
			return l
		}(),
	}
}

func NewAPIRoutingPolicyFromConfigStruct(c *config.RoutingPolicy) (*api.RoutingPolicy, error) {
	definedSets, err := config.NewAPIDefinedSetsFromConfigStruct(&c.DefinedSets)
	if err != nil {
		return nil, err
	}
	policies := make([]*api.Policy, 0, len(c.PolicyDefinitions))
	for _, policy := range c.PolicyDefinitions {
		policies = append(policies, ToPolicyApi(&policy))
	}

	return &api.RoutingPolicy{
		DefinedSets: definedSets,
		Policies:    policies,
	}, nil
}
