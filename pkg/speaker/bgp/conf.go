package bgp

import (
	"io/ioutil"
	"os"
	"path/filepath"

	bgpapi "github.com/openelb/openelb/api/v1alpha2"
	"github.com/openelb/openelb/pkg/speaker/bgp/config"
	globalpolicy "github.com/openelb/openelb/pkg/speaker/bgp/config/global"
	"github.com/openelb/openelb/pkg/speaker/bgp/table"
	api "github.com/osrg/gobgp/api"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
)

func (b *Bgp) HandleBgpGlobalConfig(global *bgpapi.BgpConf, rack string, delete bool, cm *corev1.ConfigMap) error {
	b.rack = rack

	if delete {
		return b.bgpServer.StopBgp(context.Background(), nil)
	}

	request, err := global.Spec.ToGoBgpGlobalConf()
	if err != nil {
		return err
	}

	b.bgpServer.StopBgp(context.Background(), nil)
	err = b.bgpServer.StartBgp(context.Background(), &api.StartBgpRequest{
		Global: request,
	})
	if err != nil {
		return err
	}

	if cm == nil {
		if policyConf, ok := cm.Data["conf"]; !ok {
			path, err := writeToTempFile(policyConf)
			defer os.RemoveAll(path)
			if err != nil {
				return err
			}
			newConfig, err := config.ReadConfigfile(path, "toml")
			if err != nil {
				return err
			}
			p := config.ConfigSetToRoutingPolicy(newConfig)
			rp, err := table.NewAPIRoutingPolicyFromConfigStruct(p)
			if err != nil {
				b.log.Error(err, "failed to update policy config")
			} else {
				b.bgpServer.SetPolicies(context.Background(), &api.SetPoliciesRequest{
					DefinedSets: rp.DefinedSets,
					Policies:    rp.Policies,
				})
			}
			globalpolicy.AssignGlobalpolicy(context.Background(), b.bgpServer, &newConfig.Global.ApplyPolicy.Config)
		}
	}
	return nil
}

func writeToTempFile(val string) (string, error) {
	var path string
	temp, err := ioutil.TempFile(os.TempDir(), "temp")
	if err != nil {
		return path, err
	}
	err = ioutil.WriteFile(temp.Name(), []byte(val), 0644)
	if err != nil {
		return path, err
	}
	path, err = filepath.Abs(temp.Name())
	if err != nil {
		return path, err
	}
	return path, nil
}
