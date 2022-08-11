package policy

type Policy struct {
	DefinedSets       DefinedSets        `mapstructure:"defined-sets"`
	PolicyDefinitions []PolicyDefinition `mapstructure:"policy-definitions"`
}

type DefinedSets struct {
	PrefixSets     []PrefixSet    `mapstructure:"prefix-sets" json:"prefix-sets,omitempty"`
	NeighborSets   []NeighborSet  `mapstructure:"neighbor-sets" json:"neighbor-sets,omitempty"`
	TagSets        []TagSet       `mapstructure:"tag-sets" json:"tag-sets,omitempty"`
	BgpDefinedSets BgpDefinedSets `mapstructure:"bgp-defined-sets" json:"bgp-defined-sets,omitempty"`
}

type PrefixSet struct {
	PrefixSetName string   `mapstructure:"prefix-set-name" json:"prefix-set-name,omitempty"`
	PrefixList    []Prefix `mapstructure:"prefix-list" json:"prefix-list,omitempty"`
}

type Prefix struct {
	IpPrefix        string `mapstructure:"ip-prefix" json:"ip-prefix,omitempty"`
	MasklengthRange string `mapstructure:"masklength-range" json:"masklength-range,omitempty"`
}

type NeighborSet struct {
	NeighborSetName  string   `mapstructure:"neighbor-set-name" json:"neighbor-set-name,omitempty"`
	NeighborInfoList []string `mapstructure:"neighbor-info-list" json:"neighbor-info-list,omitempty"`
}

type TagSet struct {
	TagSetName string `mapstructure:"tag-set-name" json:"tag-set-name,omitempty"`
	TagList    []Tag  `mapstructure:"tag-list" json:"tag-list,omitempty"`
}

type Tag struct {
	Value TagType `mapstructure:"value" json:"value,omitempty"`
}

type TagType string

type BgpDefinedSets struct {
	CommunitySets      []CommunitySet      `mapstructure:"community-sets" json:"community-sets,omitempty"`
	ExtCommunitySets   []ExtCommunitySet   `mapstructure:"ext-community-sets" json:"ext-community-sets,omitempty"`
	AsPathSets         []AsPathSet         `mapstructure:"as-path-sets" json:"as-path-sets,omitempty"`
	LargeCommunitySets []LargeCommunitySet `mapstructure:"large-community-sets" json:"large-community-sets,omitempty"`
}

type CommunitySet struct {
	CommunitySetName string   `mapstructure:"community-set-name" json:"community-set-name,omitempty"`
	CommunityList    []string `mapstructure:"community-list" json:"community-list,omitempty"`
}

type ExtCommunitySet struct {
	ExtCommunitySetName string   `mapstructure:"ext-community-set-name" json:"ext-community-set-name,omitempty"`
	ExtCommunityList    []string `mapstructure:"ext-community-list" json:"ext-community-list,omitempty"`
}

type AsPathSet struct {
	AsPathSetName string   `mapstructure:"as-path-set-name" json:"as-path-set-name,omitempty"`
	AsPathList    []string `mapstructure:"as-path-list" json:"as-path-list,omitempty"`
}

type LargeCommunitySet struct {
	LargeCommunitySetName string   `mapstructure:"large-community-set-name" json:"large-community-set-name,omitempty"`
	LargeCommunityList    []string `mapstructure:"large-community-list" json:"large-community-list,omitempty"`
}

type PolicyDefinition struct {
	Name       string      `mapstructure:"name" json:"name,omitempty"`
	Statements []Statement `mapstructure:"statements" json:"statements,omitempty"`
}

type Statement struct {
	Name       string     `mapstructure:"name" json:"name,omitempty"`
	Conditions Conditions `mapstructure:"conditions" json:"conditions,omitempty"`
	Actions    Actions    `mapstructure:"actions" json:"actions,omitempty"`
}

type Conditions struct {
	CallPolicy        string              `mapstructure:"call-policy" json:"call-policy,omitempty"`
	MatchPrefixSet    MatchPrefixSet      `mapstructure:"match-prefix-set" json:"match-prefix-set,omitempty"`
	MatchNeighborSet  MatchNeighborSet    `mapstructure:"match-neighbor-set" json:"match-neighbor-set,omitempty"`
	MatchTagSet       MatchTagSet         `mapstructure:"match-tag-set" json:"match-tag-set,omitempty"`
	InstallProtocolEq InstallProtocolType `mapstructure:"install-protocol-eq" json:"install-protocol-eq,omitempty"`
	BgpConditions     BgpConditions       `mapstructure:"bgp-conditions" json:"bgp-conditions,omitempty"`
}
type MatchPrefixSet struct {
	PrefixSet       string                        `mapstructure:"prefix-set" json:"prefix-set,omitempty"`
	MatchSetOptions MatchSetOptionsRestrictedType `mapstructure:"match-set-options" json:"match-set-options,omitempty"`
}

type MatchSetOptionsRestrictedType string

type MatchNeighborSet struct {
	NeighborSet     string                        `mapstructure:"neighbor-set" json:"neighbor-set,omitempty"`
	MatchSetOptions MatchSetOptionsRestrictedType `mapstructure:"match-set-options" json:"match-set-options,omitempty"`
}

type MatchTagSet struct {
	TagSet          string                        `mapstructure:"tag-set" json:"tag-set,omitempty"`
	MatchSetOptions MatchSetOptionsRestrictedType `mapstructure:"match-set-options" json:"match-set-options,omitempty"`
}

type InstallProtocolType string

type BgpConditions struct {
	MatchCommunitySet      MatchCommunitySet        `mapstructure:"match-community-set" json:"match-community-set,omitempty"`
	MatchExtCommunitySet   MatchExtCommunitySet     `mapstructure:"match-ext-community-set" json:"match-ext-community-set,omitempty"`
	MatchAsPathSet         MatchAsPathSet           `mapstructure:"match-as-path-set" json:"match-as-path-set,omitempty"`
	MedEq                  uint32                   `mapstructure:"med-eq" json:"med-eq,omitempty"`
	OriginEq               BgpOriginAttrType        `mapstructure:"origin-eq" json:"origin-eq,omitempty"`
	NextHopInList          []string                 `mapstructure:"next-hop-in-list" json:"next-hop-in-list,omitempty"`
	AfiSafiInList          []AfiSafiType            `mapstructure:"afi-safi-in-list" json:"afi-safi-in-list,omitempty"`
	LocalPrefEq            uint32                   `mapstructure:"local-pref-eq" json:"local-pref-eq,omitempty"`
	CommunityCount         CommunityCount           `mapstructure:"community-count" json:"community-count,omitempty"`
	AsPathLength           AsPathLength             `mapstructure:"as-path-length" json:"as-path-length,omitempty"`
	RouteType              RouteType                `mapstructure:"route-type" json:"route-type,omitempty"`
	RpkiValidationResult   RpkiValidationResultType `mapstructure:"rpki-validation-result" json:"rpki-validation-result,omitempty"`
	MatchLargeCommunitySet MatchLargeCommunitySet   `mapstructure:"match-large-community-set" json:"match-large-community-set,omitempty"`
}

type MatchCommunitySet struct {
	CommunitySet    string              `mapstructure:"community-set" json:"community-set,omitempty"`
	MatchSetOptions MatchSetOptionsType `mapstructure:"match-set-options" json:"match-set-options,omitempty"`
}

type MatchExtCommunitySet struct {
	ExtCommunitySet string              `mapstructure:"ext-community-set" json:"ext-community-set,omitempty"`
	MatchSetOptions MatchSetOptionsType `mapstructure:"match-set-options" json:"match-set-options,omitempty"`
}

type MatchAsPathSet struct {
	AsPathSet       string              `mapstructure:"as-path-set" json:"as-path-set,omitempty"`
	MatchSetOptions MatchSetOptionsType `mapstructure:"match-set-options" json:"match-set-options,omitempty"`
}

type BgpOriginAttrType string
type AfiSafiType string

type CommunityCount struct {
	Operator AttributeComparison `mapstructure:"operator" json:"operator,omitempty"`
	Value    uint32              `mapstructure:"value" json:"value,omitempty"`
}

type AsPathLength struct {
	Operator AttributeComparison `mapstructure:"operator" json:"operator,omitempty"`
	Value    uint32              `mapstructure:"value" json:"value,omitempty"`
}

type RouteType string
type RpkiValidationResultType string

type MatchLargeCommunitySet struct {
	LargeCommunitySet string              `mapstructure:"large-community-set" json:"large-community-set,omitempty"`
	MatchSetOptions   MatchSetOptionsType `mapstructure:"match-set-options" json:"match-set-options,omitempty"`
}

type MatchSetOptionsType string

type AttributeComparison string

type Actions struct {
	RouteDisposition RouteDisposition `mapstructure:"route-disposition" json:"route-disposition,omitempty"`
	IgpActions       IgpActions       `mapstructure:"igp-actions" json:"igp-actions,omitempty"`
	BgpActions       BgpActions       `mapstructure:"bgp-actions" json:"bgp-actions,omitempty"`
}

type RouteDisposition string

type IgpActions struct {
	SetTag TagType `mapstructure:"set-tag" json:"set-tag,omitempty"`
}

type BgpActions struct {
	SetAsPathPrepend SetAsPathPrepend  `mapstructure:"set-as-path-prepend" json:"set-as-path-prepend,omitempty"`
	SetCommunity     SetCommunity      `mapstructure:"set-community" json:"set-community,omitempty"`
	SetExtCommunity  SetExtCommunity   `mapstructure:"set-ext-community" json:"set-ext-community,omitempty"`
	SetRouteOrigin   BgpOriginAttrType `mapstructure:"set-route-origin" json:"set-route-origin,omitempty"`
	SetLocalPref     uint32            `mapstructure:"set-local-pref" json:"set-local-pref,omitempty"`
}

type SetAsPathPrepend struct {
	RepeatN uint8  `mapstructure:"repeat-n" json:"repeat-n,omitempty"`
	As      string `mapstructure:"as" json:"as,omitempty"`
}

type SetCommunity struct {
	SetCommunityMethod SetCommunityMethod `mapstructure:"set-community-method" json:"set-community-method,omitempty"`
	Options            string             `mapstructure:"options" json:"options,omitempty"`
}

type SetExtCommunity struct {
	SetExtCommunityMethod SetExtCommunityMethod `mapstructure:"set-ext-community-method" json:"set-ext-community-method,omitempty"`
	Options               string                `mapstructure:"options" json:"options,omitempty"`
}

type SetCommunityMethod struct {
	CommunitiesList []string `mapstructure:"communities-list" json:"communities-list,omitempty"`
	CommunitySetRef string   `mapstructure:"community-set-ref" json:"community-set-ref,omitempty"`
}

type SetExtCommunityMethod struct {
	CommunitiesList    []string `mapstructure:"communities-list" json:"communities-list,omitempty"`
	ExtCommunitySetRef string   `mapstructure:"ext-community-set-ref" json:"ext-community-set-ref,omitempty"`
}
