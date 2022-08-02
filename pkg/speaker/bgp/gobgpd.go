package bgp

import (
	"sync"

	"github.com/openelb/openelb/pkg/constant"
	"github.com/openelb/openelb/pkg/speaker"
	"github.com/openelb/openelb/pkg/util"
	api "github.com/osrg/gobgp/api"
	"github.com/osrg/gobgp/pkg/config"
	"github.com/osrg/gobgp/pkg/server"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ speaker.Speaker = &Bgp{}

type Client struct {
	Clientset kubernetes.Interface
}

func (c *Client) NewGoBgpd(bgpOptions *Options) *Bgp {
	maxSize := 4 << 20 //4MB
	grpcOpts := []grpc.ServerOption{grpc.MaxRecvMsgSize(maxSize), grpc.MaxSendMsgSize(maxSize)}

	bgpServer := server.NewBgpServer(server.GrpcListenAddress(bgpOptions.GrpcHosts), server.GrpcOption(grpcOpts))

	return &Bgp{
		bgpServer: bgpServer,
		client:    *c,
		log:       ctrl.Log.WithName("bgpserver"),
	}
}

func (b *Bgp) InitGoBgpConf() error {
	ctrl.Log.Info("gert com", "env", util.EnvNamespace())
	cmClient := b.client.Clientset.CoreV1().ConfigMaps(util.EnvNamespace())
	cm, err := cmClient.Get(context.TODO(), constant.OpenELBBgpConfigMap, metav1.GetOptions{})
	ctrl.Log.Info("ran client", "cmclient", cm, "err", err)
	if err != nil {
		b.log.Error(err, "error finding ConfigMap", "cm", constant.OpenELBBgpConfigMap)
		return err
	}
	path, err := WriteToTempFile(cm.Data["conf"])
	if err != nil {
		return err
	}
	initialConfig, err := config.ReadConfigFile(path, "toml")
	if err != nil {
		return err
	}
	_, err = config.InitialConfig(context.Background(), b.bgpServer, initialConfig, false)
	return err
}

func (b *Bgp) run(stopCh <-chan struct{}) {
	log := ctrl.Log.WithName("gobgpd")
	log.Info("gobgpd starting")
	go b.bgpServer.Serve()
	<-stopCh
	log.Info("gobgpd ending")
	err := b.bgpServer.StopBgp(context.Background(), &api.StopBgpRequest{})
	if err != nil {
		log.Error(err, "failed to stop gobgpd")
	}
}

func (b *Bgp) Start(stopCh <-chan struct{}) error {
	go b.run(stopCh)
	return nil
}

type cache struct {
	lock  sync.Mutex
	confs map[string]interface{}
}

var (
	confs *cache
	peers *cache
)

func (c *cache) set(k string, v interface{}) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.confs[k] = v
}

func (c *cache) delete(k string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.confs, k)
}

func (c *cache) get(k string) interface{} {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.confs[k]
}

func init() {
	confs = &cache{
		lock:  sync.Mutex{},
		confs: make(map[string]interface{}),
	}
	peers = &cache{
		lock:  sync.Mutex{},
		confs: make(map[string]interface{}),
	}
}
