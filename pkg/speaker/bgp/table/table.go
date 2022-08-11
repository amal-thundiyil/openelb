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
)

type ConditionType int

type RouteType int

type PolicyOptions struct {
	Info       *PeerInfo
	OldNextHop net.IP
	Validate   func(*Path) *Validation
}

type PolicyDirection int

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

const (
	POLICY_DIRECTION_NONE PolicyDirection = iota
	POLICY_DIRECTION_IMPORT
	POLICY_DIRECTION_EXPORT
)

const (
	ROUTE_TYPE_NONE RouteType = iota
	ROUTE_TYPE_ACCEPT
	ROUTE_TYPE_REJECT
)

type ActionType int

type Action interface {
	Type() ActionType
	Apply(*Path, *PolicyOptions) (*Path, error)
	String() string
}

type DefinedType int

type DefinedSet interface {
	Type() DefinedType
	Name() string
	Append(DefinedSet) error
	Remove(DefinedSet) error
	Replace(DefinedSet) error
	String() string
	List() []string
}

type Condition interface {
	Name() string
	Type() ConditionType
	Evaluate(*Path, *PolicyOptions) bool
	Set() DefinedSet
}

type Policy struct {
	Name       string
	Statements []*Statement
}

type Statement struct {
	Name        string
	Conditions  []Condition
	RouteAction Action
	ModActions  []Action
}

type PolicyAssignment struct {
	Name     string
	Type     PolicyDirection
	Policies []*Policy
	Default  RouteType
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

type MatchOption int

type MatchSetOptionsType string

const (
	MATCH_SET_OPTIONS_TYPE_ANY    MatchSetOptionsType = "any"
	MATCH_SET_OPTIONS_TYPE_ALL    MatchSetOptionsType = "all"
	MATCH_SET_OPTIONS_TYPE_INVERT MatchSetOptionsType = "invert"
)

const (
	MATCH_OPTION_ANY MatchOption = iota
	MATCH_OPTION_ALL
	MATCH_OPTION_INVERT
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

type Prefix struct {
	Prefix             *net.IPNet
	AddressFamily      bgp.RouteFamily
	MasklengthRangeMax uint8
	MasklengthRangeMin uint8
}

type PrefixSet struct {
	name   string
	tree   *critbitgo.Net
	family bgp.RouteFamily
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
				case *RpkiValidationCondition:
					cond.BgpConditions.RpkiValidationResult = v.result
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
