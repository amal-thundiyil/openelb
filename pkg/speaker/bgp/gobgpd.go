package bgp

import (
	"github.com/openelb/openelb/pkg/speaker"
	"github.com/osrg/gobgp/pkg/server"
	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ speaker.Speaker = &Bgp{}

type Client struct {
	Clientset kubernetes.Interface
}

func (c *Client) NewGoBgpd(bgpOptions *BgpOptions) *Bgp {
	maxSize := 4 << 20 //4MB
	grpcOpts := []grpc.ServerOption{grpc.MaxRecvMsgSize(maxSize), grpc.MaxSendMsgSize(maxSize)}

	bgpServer := server.NewBgpServer(server.GrpcListenAddress(bgpOptions.GrpcHosts), server.GrpcOption(grpcOpts))

	return &Bgp{
		bgpServer: bgpServer,
		client:    *c,
		log:       ctrl.Log.WithName("bgpserver"),
	}
}
