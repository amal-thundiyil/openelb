package bgp

import (
	"fmt"
	"net"

	"context"

	bgpapi "github.com/openelb/openelb/api/v1alpha2"
	"github.com/openelb/openelb/pkg/metrics"
	"github.com/openelb/openelb/pkg/util"
	ctrl "sigs.k8s.io/controller-runtime"

	api "github.com/osrg/gobgp/api"
)

func defaultFamily(ip net.IP) *bgpapi.Family {
	family := &bgpapi.Family{
		Afi:  api.Family_AFI_IP.String(),
		Safi: api.Family_SAFI_UNICAST.String(),
	}
	if ip.To4() == nil {
		family = &bgpapi.Family{
			Afi:  api.Family_AFI_IP6.String(),
			Safi: api.Family_SAFI_UNICAST.String(),
		}
	}

	return family
}

func (b *Bgp) HandleBgpPeerStatus(bgpPeers []bgpapi.BgpPeer) []*bgpapi.BgpPeer {
	var (
		result []*bgpapi.BgpPeer
		dels   []*api.Peer
	)

	fn := func(peer *api.Peer) {
		tmp, err := bgpapi.GetStatusFromGoBgpPeer(peer)
		if err != nil {
			ctrl.Log.Error(err, "failed to ConverStatusFromGoBgpPeer", "peer", peer)
			return
		}

		var found *bgpapi.BgpPeer

		for _, bgpPeer := range bgpPeers {
			if bgpPeer.Spec.Conf.NeighborAddress == tmp.PeerState.NeighborAddress {
				found = &bgpPeer
				break
			}
		}

		if found == nil {
			dels = append(dels, peer)
		} else {
			clone := found.DeepCopy()
			if clone.Status.NodesPeerStatus == nil {
				clone.Status.NodesPeerStatus = make(map[string]bgpapi.NodePeerStatus)
			}

			if clone.Spec.Conf.NeighborAddress == tmp.PeerState.NeighborAddress {
				clone.Status.NodesPeerStatus[util.GetNodeName()] = tmp
			}

			result = append(result, clone)
		}
	}
	b.bgpServer.ListPeer(context.Background(), &api.ListPeerRequest{
		Address: "",
	}, fn)

	for _, del := range dels {
		ctrl.Log.Info("delete useless bgp peer", "peer", del)
		b.bgpServer.DeletePeer(context.Background(), &api.DeletePeerRequest{
			Address:   del.Conf.NeighborAddress,
			Interface: del.Conf.NeighborInterface,
		})
	}

	return result
}

func (b *Bgp) GetBgpConfStatus() bgpapi.BgpConf {
	result, err := b.bgpServer.GetBgp(context.Background(), nil)
	if err != nil {
		ctrl.Log.Error(err, "failed to get bgpconf status")
		return bgpapi.BgpConf{
			Status: bgpapi.BgpConfStatus{
				NodesConfStatus: map[string]bgpapi.NodeConfStatus{
					util.GetNodeName(): bgpapi.NodeConfStatus{
						RouterId: "",
						As:       0,
					},
				},
			},
		}
	}
	return bgpapi.BgpConf{
		Status: bgpapi.BgpConfStatus{
			NodesConfStatus: map[string]bgpapi.NodeConfStatus{
				util.GetNodeName(): bgpapi.NodeConfStatus{
					RouterId: result.Global.RouterId,
					As:       result.Global.As,
				},
			},
		},
	}
}

func (b *Bgp) HandleBgpPeer(neighbor *bgpapi.BgpPeer, delete bool) error {
	// set default afisafi
	if len(neighbor.Spec.AfiSafis) == 0 {
		ip := net.ParseIP(neighbor.Spec.Conf.NeighborAddress)
		if ip == nil {
			return fmt.Errorf("field Spec.Conf.NeighborAddress invalid")
		}
		neighbor.Spec.AfiSafis = append(neighbor.Spec.AfiSafis, &bgpapi.AfiSafi{
			Config: &bgpapi.AfiSafiConfig{
				Family:  defaultFamily(ip),
				Enabled: true,
			},
			AddPaths: &bgpapi.AddPaths{
				Config: &bgpapi.AddPathsConfig{
					SendMax: 10,
				},
			},
		})
	}

	request, e := neighbor.Spec.ToGoBgpPeer()
	if e != nil {
		return e
	}

	b.SetPeerMetrics()
	if delete {
		b.bgpServer.DeletePeer(context.Background(), &api.DeletePeerRequest{
			Address:   request.Conf.NeighborAddress,
			Interface: request.Conf.NeighborInterface,
		})
	} else {
		_, e = b.bgpServer.UpdatePeer(context.Background(), &api.UpdatePeerRequest{
			Peer: request,
		})
		if e != nil {
			return b.bgpServer.AddPeer(context.Background(), &api.AddPeerRequest{
				Peer: request,
			})
		}
	}

	return nil
}

func (b *Bgp) SetPeerMetrics() {
	nodeMap, staleNodeMap := b.filterPeers(b.getPeers())
	initPeerMetrics(nodeMap)
	updatePeerMetrics(nodeMap)
	deletePeerMetrics(staleNodeMap)
}

func (b *Bgp) getPeers() []*api.Peer {
	peerList := []*api.Peer{}
	fn := func(p *api.Peer) {
		peerList = append(peerList, p)
	}
	err := b.bgpServer.ListPeer(context.Background(), &api.ListPeerRequest{}, fn)
	if err != nil {
		return nil
	}
	return peerList
}

func (b *Bgp) filterPeers(peers []*api.Peer) (map[string]api.Peer, map[string]api.Peer) {
	staleNodeMap := make(map[string]api.Peer)
	nodeMap := make(map[string]api.Peer)
	fn := func(peer *api.Peer) {
		var found *api.Peer
		for _, peer := range peers {
			if peer.Conf.NeighborAddress == peer.State.NeighborAddress {
				found = peer
				break
			}
		}
		if found == nil {
			staleNodeMap[util.GetNodeName()] = *peer
		} else if found.Conf.NeighborAddress == found.State.NeighborAddress {
			nodeMap[util.GetNodeName()] = *found
		}
	}
	b.bgpServer.ListPeer(context.Background(), &api.ListPeerRequest{
		Address: "",
	}, fn)
	return nodeMap, staleNodeMap
}

func initPeerMetrics(nodes map[string]api.Peer) {
	for node, peer := range nodes {
		peerIP := peer.Conf.NeighborAddress
		if node != util.GetNodeName() {
			continue
		}
		metrics.InitBGPPeerMetrics(peerIP, node)
	}
}

func updatePeerMetrics(nodes map[string]api.Peer) {
	for node, peer := range nodes {
		peerIP := peer.Conf.NeighborAddress
		if node != util.GetNodeName() {
			continue
		}
		state := float64(peer.State.SessionState)
		updateCount := float64(peer.State.Messages.Received.Update)
		metrics.UpdateBGPSessionMetrics(peerIP, node, state, updateCount)
	}
}

func deletePeerMetrics(staleNodes map[string]api.Peer) {
	for node, peer := range staleNodes {
		peerIP := peer.Conf.NeighborAddress
		metrics.DeleteBGPPeerMetrics(peerIP, node)
	}
}
