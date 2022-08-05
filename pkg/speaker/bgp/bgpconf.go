package bgp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/openelb/openelb/pkg/constant"
	"github.com/openelb/openelb/pkg/util"
	api "github.com/osrg/gobgp/api"
	"github.com/osrg/gobgp/pkg/config"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type BgpConfig struct {
	Args []string
}

func (b *Bgp) SetBalancer(configMap string, nexthops []corev1.Node) error {
	ip := strings.SplitN(configMap, ":", 2)
	b.cm = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      constant.OpenELBBgpConfigMap,
			Namespace: util.EnvNamespace(),
		},
		Data: map[string]string{
			ip[0]: ip[1],
		},
	}
	b.log.Info(fmt.Sprintf("cm %s", b.cm.String()))
	var err error
	if oldCm, err := b.client.Clientset.CoreV1().ConfigMaps(b.cm.ObjectMeta.Namespace).Get(context.TODO(), b.cm.ObjectMeta.Name, metav1.GetOptions{}); errors.IsNotFound(err) {
		b.cm, err = b.client.Clientset.CoreV1().ConfigMaps(b.cm.ObjectMeta.Namespace).Create(context.TODO(), b.cm, metav1.CreateOptions{})
		b.log.Info(fmt.Sprintf("create cm %s", b.cm.ObjectMeta.Name))
		if err != nil {
			b.log.Error(err, "create cm error")
		}
	} else {
		for oldk, oldv := range oldCm.Data {
			if _, ok := b.cm.Data[oldk]; !ok {
				b.cm.Data[oldk] = oldv
			}
		}
		b.cm, err = b.client.Clientset.CoreV1().ConfigMaps(b.cm.Namespace).Update(context.TODO(), b.cm, metav1.UpdateOptions{})
		if err != nil {
			b.log.Error(err, "error updating cm")
		}
	}

	return err
}

func (b *Bgp) DelBalancer(configMap string) error {
	var err error
	if _, err = b.client.Clientset.CoreV1().ConfigMaps(b.cm.ObjectMeta.Namespace).Get(context.TODO(), b.cm.ObjectMeta.Name, metav1.GetOptions{}); err == nil {
		ip := strings.SplitN(configMap, ":", 2)
		delete(b.cm.Data, ip[0])
		b.cm, err = b.client.Clientset.CoreV1().ConfigMaps(b.cm.ObjectMeta.Namespace).Update(context.TODO(), b.cm, metav1.UpdateOptions{})
	}
	return err
}

func (b *Bgp) Start(stopCh <-chan struct{}) error {
	dsClient := b.client.Clientset.AppsV1().DaemonSets(util.EnvNamespace())
	ds, err := dsClient.Get(context.TODO(), constant.OpenELBBgpName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			ds, err = dsClient.Create(context.TODO(), b.generateBgpDaemonSet(), metav1.CreateOptions{})
			if err != nil {
				b.log.Error(err, "Bgp daemonSet create error")
				return err
			}
			b.log.Info(fmt.Sprintf("Bgp daemonSet %s created successfully", ds.Name))
		} else {
			b.log.Error(err, "Bgp daemonSet get error")
			return err
		}
	}
	b.log.Info("starting server")
	go b.bgpServer.Serve()

	// get and initalize bgp server from configmap
	dir := ds.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath
	file := ds.Spec.Template.Spec.Volumes[0].ConfigMap.Name
	if cm, err := b.getConfig(constant.OpenELBBgpConfigMap); err == nil {
		err = b.InitGoBgpConf(filepath.Join(dir, file))
		if err != nil {
			b.log.Error(err, "failed to initalize with config", "cm", cm)
			return err
		}
	} else {
		b.log.Info("Bgp config map %s not found", constant.OpenELBBgpConfigMap)
	}

	go func() {
		select {
		case <-stopCh:
			err := b.bgpServer.StopBgp(context.Background(), &api.StopBgpRequest{})
			if err != nil {
				b.log.Error(err, "failed to stop erver")
			}
			deletePolicy := metav1.DeletePropagationForeground
			if err = dsClient.Delete(context.TODO(), ds.Name, metav1.DeleteOptions{
				PropagationPolicy: &deletePolicy,
			}); err != nil {
				b.log.Error(err, "Bgp daemonSet delete error")
			}
		}
	}()

	return err
}

func (b *Bgp) InitGoBgpConf(path string) error {
	initialConfig, err := config.ReadConfigFile(path, "toml")
	ctrl.Log.Info("amal: initial config", "config", initialConfig, "err", err)
	if err != nil {
		return err
	}
	ctrl.Log.Info("amal: config not initialized", "config", initialConfig, "err", err)
	x, err := config.InitialConfig(context.Background(), b.bgpServer, initialConfig, true)
	ctrl.Log.Info("amal: config initialized", "config", x, "err", err)
	return err
}

// User can config Bgp by ConfigMap to specify the images
// If the ConfigMap exists and the configuration is set, use it,
// otherwise, use the default image got from constants.
func (b *Bgp) getConfig(cmName string) (*corev1.ConfigMap, error) {
	return b.client.Clientset.CoreV1().ConfigMaps(util.EnvNamespace()).
		Get(context.Background(), cmName, metav1.GetOptions{})
}

func (b *Bgp) getImage() string {
	cm, err := b.getConfig(constant.OpenELBImagesConfigMap)
	if err != nil {
		b.log.Info("using default image %s", constant.OpenELBDefaultBgpImage)
		return constant.OpenELBDefaultBgpImage
	}
	image, exist := cm.Data[constant.OpenELBBgpImage]
	if !exist {
		b.log.Info("using default image %s", constant.OpenELBDefaultBgpImage)
		return constant.OpenELBDefaultBgpImage
	}
	return image
}

func (b *Bgp) generateBgpDaemonSet() *appv1.DaemonSet {
	var privileged = true
	return &appv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constant.OpenELBBgpName,
			Namespace: util.EnvNamespace(),
			Labels: map[string]string{
				"app": constant.OpenELBBgpName,
			},
		},
		Spec: appv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": constant.OpenELBBgpName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"app": constant.OpenELBBgpName,
				}},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "clusters-config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "clusters-config",
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Image:           b.getImage(),
							Name:            constant.OpenELBBgpName,
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/clusters-config",
									Name:      "clusters-config-volume",
								},
							},
						},
					},
				},
			},
		},
	}
}
