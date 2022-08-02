package bgp

import (
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	bgpapi "github.com/openelb/openelb/api/v1alpha2"
	"github.com/openelb/openelb/pkg/constant"
	"github.com/openelb/openelb/pkg/manager"
	"github.com/openelb/openelb/pkg/manager/client"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	b       *Bgp
	testEnv *envtest.Environment
	stopCh  chan struct{}
	cfg     *rest.Config
)

var (
	cm = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      constant.OpenELBBgpConfigMap,
			Namespace: constant.OpenELBNamespace,
		},
		Data: map[string]string{},
	}

	ns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: constant.OpenELBNamespace,
		},
	}
)

func TestServerd(t *testing.T) {
	RegisterFailHandler(Fail)
	stopCh = make(chan struct{})
	log := zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter))
	ctrl.SetLogger(log)
	RunSpecs(t, "gobgpd Suite")
}

var _ = BeforeSuite(func(done Done) {
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	// +kubebuilder:scaffold:scheme

	mgr, err := manager.NewManager(cfg, &manager.GenericOptions{
		WebhookPort:   443,
		MetricsAddr:   "0",
		ReadinessAddr: "0",
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(mgr).ToNot(BeNil())

	conf, err := os.ReadFile("test.toml")
	Expect(err).ToNot(HaveOccurred())

	err = client.Client.Create(context.Background(), ns)
	Expect(err).ToNot(HaveOccurred())

	cm.Data = map[string]string{"conf": string(conf)}
	err = client.Client.Create(context.Background(), cm)
	Expect(err).ToNot(HaveOccurred())

	bgpOptions := &Options{
		GrpcHosts: ":50052",
	}

	c := Client{
		Clientset: fake.NewSimpleClientset(),
	}
	b = c.NewGoBgpd(bgpOptions)
	b.Start(stopCh)

	go func() {
		err := mgr.Start(stopCh)
		if err != nil {
			ctrl.Log.Error(err, "failed to start manager")
		}
	}()

	err = client.Client.Create(context.Background(), ns)
	Expect(err).ToNot(HaveOccurred())

	err = client.Client.Create(context.Background(), cm)
	Expect(err).ToNot(HaveOccurred())

	SetDefaultEventuallyTimeout(3 * time.Second)

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	close(stopCh)
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

var _ = Describe("BGP test", func() {
	Context("Create/Update/Delete BgpConf", func() {
		It("Add BgpConf", func() {
			err := b.HandleBgpGlobalConfig(&bgpapi.BgpConf{
				Spec: bgpapi.BgpConfSpec{
					As:         65003,
					RouterId:   "10.0.255.254",
					ListenPort: 17900,
				},
			}, "", false)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("Update BgpConf", func() {
			err := b.HandleBgpGlobalConfig(&bgpapi.BgpConf{
				Spec: bgpapi.BgpConfSpec{
					As:         65002,
					RouterId:   "10.0.255.253",
					ListenPort: 17902,
				},
			}, "", false)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("Delete BgpConf", func() {
			err := b.HandleBgpGlobalConfig(&bgpapi.BgpConf{
				Spec: bgpapi.BgpConfSpec{
					RouterId: "10.0.255.254",
				},
			}, "", true)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("Create/Update/Delete BgpPeer", func() {
		It("Add BgpPeer", func() {
			Expect(b.HandleBgpPeer(&bgpapi.BgpPeer{
				Spec: bgpapi.BgpPeerSpec{
					Conf: &bgpapi.PeerConf{
						PeerAs:          65001,
						NeighborAddress: "192.168.0.2",
					},
				},
			}, false)).Should(HaveOccurred())
		})

		It("Add BgpConf", func() {
			err := b.HandleBgpGlobalConfig(&bgpapi.BgpConf{
				Spec: bgpapi.BgpConfSpec{
					As:         65003,
					RouterId:   "10.0.255.254",
					ListenPort: 17900,
				},
			}, "", false)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("Update BgpPeer", func() {
			peer := &bgpapi.BgpPeer{
				Spec: bgpapi.BgpPeerSpec{
					Conf: &bgpapi.PeerConf{
						PeerAs:          65002,
						NeighborAddress: "192.168.0.2",
					},
				},
			}
			Expect(b.HandleBgpPeer(peer, false)).ShouldNot(HaveOccurred())

			peer.Spec.Conf.PeerAs = 65001
			Expect(b.HandleBgpPeer(peer, false)).ShouldNot(HaveOccurred())
		})

		It("Delete BgpPeer", func() {
			Expect(b.HandleBgpPeer(&bgpapi.BgpPeer{
				Spec: bgpapi.BgpPeerSpec{
					Conf: &bgpapi.PeerConf{
						NeighborAddress: "192.168.0.2",
					},
				},
			}, true)).ShouldNot(HaveOccurred())
		})

		Context("Reconcile Routes", func() {
			It("Add BgpPeer", func() {
				Expect(b.HandleBgpPeer(&bgpapi.BgpPeer{
					Spec: bgpapi.BgpPeerSpec{
						Conf: &bgpapi.PeerConf{
							PeerAs:          65001,
							NeighborAddress: "192.168.0.2",
						},
					},
				}, false)).ShouldNot(HaveOccurred())
			})

			It("Should correctly add/delete all routes", func() {
				ip := "100.100.100.100"
				nexthops := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}

				By("Init bgp should be empty")
				err, toAdd, toDelete := b.retriveRoutes(ip, 32, nexthops)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(toAdd)).Should(Equal(3))
				Expect(len(toDelete)).Should(Equal(0))

				By("Add nexthops to bgp")
				err = b.setBalancer(ip, nexthops)
				Expect(err).ShouldNot(HaveOccurred())
				err, toAdd, toDelete = b.retriveRoutes(ip, 32, nexthops)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(toAdd)).Should(Equal(0))
				Expect(len(toDelete)).Should(Equal(0))

				By("Append a nexthop to bgp")
				nexthops = append(nexthops, "4.4.4.4")
				Expect(len(nexthops)).Should(Equal(4))
				err = b.setBalancer(ip, nexthops)
				Expect(err).ShouldNot(HaveOccurred())
				err, toAdd, toDelete = b.retriveRoutes(ip, 32, nexthops)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(toAdd)).Should(Equal(0))
				Expect(len(toDelete)).Should(Equal(0))

				By("Delete two nexthops from bgp")
				nexthops = nexthops[:len(nexthops)-2]
				Expect(len(nexthops)).Should(Equal(2))
				err = b.setBalancer(ip, nexthops)
				Expect(err).ShouldNot(HaveOccurred())
				err, toAdd, toDelete = b.retriveRoutes(ip, 32, nexthops)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(toAdd)).Should(Equal(0))
				Expect(len(toDelete)).Should(Equal(0))

				By("Delete all nexthops from bgp")
				Expect(b.DelBalancer(ip)).ShouldNot(HaveOccurred())
				err, toAdd, toDelete = b.retriveRoutes(ip, 32, nexthops)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(toAdd)).Should(Equal(2))
				Expect(len(toDelete)).Should(Equal(0))
			})
		})
	})
})
