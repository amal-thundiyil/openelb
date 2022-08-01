package bgp

import (
	"github.com/go-logr/logr"
	"github.com/osrg/gobgp/pkg/server"
	"github.com/spf13/pflag"
	clientset "k8s.io/client-go/kubernetes"
)

type Options struct {
	GrpcHosts string `long:"api-hosts" description:"specify the hosts that gobgpd listens on" default:":50051"`
}

func NewOptions() *Options {
	return &Options{
		GrpcHosts: ":50051",
	}
}

func (options *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&options.GrpcHosts, "api-hosts", options.GrpcHosts, "specify the hosts that gobgpd listens on")
}

type Bgp struct {
	bgpServer *server.BgpServer
	conf      string
	clientset *clientset.Clientset
	rack      string
	log       logr.Logger
}
