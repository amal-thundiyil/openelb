package bgp

import (
	"context"
	"sync"

	"github.com/openelb/openelb/pkg/constant"
	"github.com/openelb/openelb/pkg/util"
	"github.com/osrg/gobgp/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (b *Bgp) InitGoBgpConf(path string) error {
	ctrl.Log.Info("amal: path to file", "path", path)
	initialConfig, err := config.ReadConfigFile(path, "toml")
	ctrl.Log.Info("amal: initial config", "config", initialConfig, "err", err)
	if err != nil {
		initialConfig, err := config.ReadConfigFile("clusters-config-file", "toml")
		ctrl.Log.Info("amal: trying again initial config", "config", initialConfig, "err", err)
		return err
	}
	ctrl.Log.Info("amal: config not initialized", "config", initialConfig, "err", err)
	x, err := config.InitialConfig(context.Background(), b.bgpServer, initialConfig, true)
	ctrl.Log.Info("amal: config initialized", "config", x, "err", err)
	return err
}

func (b *Bgp) watchForChanges(mutex *sync.Mutex) {
	for {
		watcher, err := b.client.Clientset.CoreV1().ConfigMaps(util.EnvNamespace()).Watch(context.TODO(),
			metav1.SingleObject(metav1.ObjectMeta{Name: constant.OpenELBBgpConfigMap, Namespace: util.EnvNamespace()}))
		if err != nil {
			panic("Unable to create watcher")
		}
		b.updateCurrentEndpoint(watcher.ResultChan(), mutex)
	}
}

func (b *Bgp) updateCurrentEndpoint(eventChannel <-chan watch.Event, mutex *sync.Mutex) {
	for {
		event, open := <-eventChannel
		if open {
			switch event.Type {
			case watch.Added:
				fallthrough
			case watch.Modified:
				mutex.Lock()
				// Update our endpoint
				if updatedMap, ok := event.Object.(*corev1.ConfigMap); ok {
					if endpointKey, ok := updatedMap.Data["current.target"]; ok {
						if targetEndpoint, ok := updatedMap.Data[endpointKey]; ok {
							*endpoint = targetEndpoint
						}
					}
				}
				mutex.Unlock()
			case watch.Deleted:
				mutex.Lock()
				// Fall back to the default value
				*endpoint = DEFAULT_ENDPOINT
				mutex.Unlock()
			default:
				// Do nothing
			}
		} else {
			// If eventChannel is closed, it means the server has closed the connection
			return
		}
	}
}
