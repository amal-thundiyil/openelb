package bgp

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/openelb/openelb/api/v1alpha2"
	"github.com/osrg/gobgp/pkg/config"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
)

func (b *Bgp) UpdateConfig(ctx context.Context, policyCM *corev1.ConfigMap, bgpConf *v1alpha2.BgpConf) error {
	// we need a file to interact with the gobgp's internal config
	// read the previous config
	prevPath, err := WriteToTempFile(b.conf)
	if err != nil {
		os.Remove(prevPath)
		return err
	}
	defer os.Remove(prevPath)
	prevConf, err := config.ReadConfigFile(prevPath, "toml")
	if err != nil {
		return err
	}
	// read the new config
	newPath, err := WriteToTempFile(policyCM.Data["conf"])
	if err != nil {
		return err
	}
	defer os.Remove(newPath)
	newConf, err := config.ReadConfigFile(newPath, "toml")
	if err != nil {
		return err
	}
	// update config and store
	b.conf = policyCM.Data["conf"]
	_, err = config.UpdateConfig(ctx, b.bgpServer, prevConf, newConf)
	return err
}

func WriteToTempFile(val string) (string, error) {
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
