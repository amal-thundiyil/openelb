package bgp

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/openelb/openelb/pkg/constant"
	"github.com/openelb/openelb/pkg/util"

	"github.com/osrg/gobgp/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (b *Bgp) watchForChanges(mutex *sync.Mutex) {
	for {
		watcher, err := b.client.Clientset.CoreV1().ConfigMaps(util.EnvNamespace()).Watch(context.TODO(),
			metav1.SingleObject(metav1.ObjectMeta{Name: constant.OpenELBBgpConfigMap, Namespace: util.EnvNamespace()}))
		if err != nil {
			panic("unable to create watcher")
		}
		b.updateConfigMap(watcher.ResultChan(), mutex)
	}
}

func (b *Bgp) updateConfigMap(eventChannel <-chan watch.Event, mutex *sync.Mutex) {
	for {
		event, open := <-eventChannel
		ctrl.Log.Info("get me all the events", "event", event, "type", event.Type)
		if open {
			switch event.Type {
			case watch.Added:
				if addedMap, ok := event.Object.(*corev1.ConfigMap); ok {
					err := b.initialConfig(addedMap)
					if err != nil {
						b.log.Error(err, "error initalizing gobgp config")
						return
					}
				}
			case watch.Modified:
				if updatedMap, ok := event.Object.(*corev1.ConfigMap); ok {
					err := b.updateConfig(updatedMap)
					if err != nil {
						b.log.Error(err, "error updating gobgp config")
						return
					}
				}
			case watch.Deleted:
				err := b.bgpServer.StopBgp(context.Background(), nil)
				if err != nil {
					b.log.Error(err, "error stopping bgp server")
					return
				}
			}
		} else {
			// If eventChannel is closed, it means the server has closed the connection
			return
		}
	}
}

func (b *Bgp) initialConfig(cm *corev1.ConfigMap) error {
	data, ok := cm.Data[constant.OpenELBBgpName]
	if !ok {
		return fmt.Errorf("no gobgp config found")
	}
	ctrl.Log.Info("amal: initial config", "conf", b.conf)
	ctrl.Log.Info("amal: initial config", "data", data)
	path, err := writeToTempFile(data)
	ctrl.Log.Info("amal: initial config temp file writter", "path", path, "err", err)
	defer os.RemoveAll(path)
	if err != nil {
		return err
	}
	initialConfig, err := config.ReadConfigFile(path, "toml")
	ctrl.Log.Info("amal: initial config read", "initconfig", initialConfig)
	if err != nil {
		return err
	}
	_, err = config.InitialConfig(context.Background(), b.bgpServer, initialConfig, false)
	ctrl.Log.Info("amal: initial config written", "err", err)
	if err == nil {
		b.conf = data
	}
	ctrl.Log.Info("amal: initial config written", "bgp", b)
	return err
}

func (b *Bgp) updateConfig(cm *corev1.ConfigMap) error {
	data, ok := cm.Data[constant.OpenELBBgpName]
	ctrl.Log.Info("amal: initial config", "conf", b.conf)
	ctrl.Log.Info("amal: initial config", "data", data)
	if !ok {
		return fmt.Errorf("no gobgp config found")
	}
	// read old config
	prevPath, err := writeToTempFile(b.conf)
	ctrl.Log.Info("amal: prev config temp file writter", "path", prevPath, "err", err)
	defer os.RemoveAll(prevPath)
	if err != nil {
		return err
	}
	prevConf, err := config.ReadConfigFile(prevPath, "toml")
	ctrl.Log.Info("amal: prev config read", "conf", prevConf)
	if err != nil {
		return err
	}
	// read the new config
	newPath, err := writeToTempFile(data)
	defer os.RemoveAll(newPath)
	if err != nil {
		return err
	}
	newConf, err := config.ReadConfigFile(newPath, "toml")
	ctrl.Log.Info("amal: new config read", "conf", newConf)
	if err != nil {
		return err
	}
	_, err = config.UpdateConfig(context.Background(), b.bgpServer, prevConf, newConf)
	ctrl.Log.Info("amal: new config updated", "conf", newConf)
	if err == nil {
		b.conf = data
	}
	return err
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
