package config

import (
	"github.com/spf13/viper"
	ctrl "sigs.k8s.io/controller-runtime"
)

type BgpConfigSet struct {
	Global            Global             `mapstructure:"global"`
	DefinedSets       DefinedSets        `mapstructure:"defined-sets"`
	PolicyDefinitions []PolicyDefinition `mapstructure:"policy-definitions"`
}

func ReadConfigfile(path, format string) (*BgpConfigSet, error) {
	config := &BgpConfigSet{}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType(format)
	var err error
	if err = v.ReadInConfig(); err != nil {
		return nil, err
	}
	if err = v.UnmarshalExact(config); err != nil {
		return nil, err
	}
	ctrl.Log.Info("amal: unmarsheldj", "config", config, "error", err)
	return config, nil
}

func ConfigSetToRoutingPolicy(c *BgpConfigSet) *RoutingPolicy {
	return &RoutingPolicy{
		DefinedSets:       c.DefinedSets,
		PolicyDefinitions: c.PolicyDefinitions,
	}
}
