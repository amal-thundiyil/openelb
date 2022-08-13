package bgp

import (
	bgpapi "github.com/openelb/openelb/api/v1alpha2"
	api "github.com/osrg/gobgp/api"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
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

	if cm != nil {
		ctrl.Log.Info("configmap found, going to update now")
		b.UpdatePolicy(cm)
	}
	return nil
}
