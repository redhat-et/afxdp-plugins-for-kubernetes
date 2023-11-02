package bpfd

import (
	"context"
	"os"
	"time"

	bpfdiov1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/apis/v1alpha1"
	bpfdclient "github.com/bpfd-dev/bpfd/bpfd-operator/pkg/client/clientset/versioned"
	v1alpha1 "github.com/bpfd-dev/bpfd/bpfd-operator/pkg/client/clientset/versioned/typed/apis/v1alpha1"
	"github.com/intel/afxdp-plugins-for-kubernetes/constants"
	"github.com/pkg/errors"
	logging "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
)

// Definitions not available in bpfd 2.0
const (
	BpfProgCondLoaded       = "Loaded"
	ProgramReconcileSuccess = "ReconcileSuccess"
	ProgramReconcileError   = "ReconcileError"
)

type BpfdClient struct {
	bpfdClientset     *bpfdclient.Clientset
	xdpProgramsClient v1alpha1.XdpProgramInterface
	bpfProgramsClient v1alpha1.BpfProgramInterface
}

var (
	nodeName = os.Getenv("HOSTNAME")
)

func NewBpfdClient() *BpfdClient {
	config, err := rest.InClusterConfig()
	if err != nil {
		logging.Errorf("error retrieving the in cluster configuration: %v", err)
		return nil
	}

	// Create the bpfd clientset
	clientset, err := bpfdclient.NewForConfig(config)
	if err != nil {
		logging.Errorf("Failed to create the bpfd clientset: %v", err)
		return nil
	}

	bpfdv1alpha := clientset.BpfdV1alpha1()

	bc := &BpfdClient{
		bpfdClientset:     clientset,
		xdpProgramsClient: bpfdv1alpha.XdpPrograms(),
		bpfProgramsClient: bpfdv1alpha.BpfPrograms(),
	}

	logging.Debug("Created a new BpfdClient")

	return bc
}

func (b *BpfdClient) GetXdpProgs() (*bpfdiov1alpha1.XdpProgramList, error) {
	// Get the xdp program resources
	xdpProgramList, err := b.xdpProgramsClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to retrieve the xdp program resources: %v", err.Error())
	}

	if len(xdpProgramList.Items) == 0 {
		logging.Infof("No XDP programs found\n")
		return nil, nil
	}

	return xdpProgramList, nil
}

func (b *BpfdClient) GetBPFProgs() (*bpfdiov1alpha1.BpfProgramList, error) {
	// Get the xdp program resources
	bpfProgramList, err := b.bpfProgramsClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to retrieve the xdp program resources: %v", err.Error())
	}

	if len(bpfProgramList.Items) == 0 {
		logging.Infof("No BPF programs found\n")
		return nil, nil
	}

	return bpfProgramList, nil
}

func (b *BpfdClient) SubmitXdpProg(iface, node, pm, image, sec string) (string, error) {
	var (
		name       = node + "-" + pm + "-" + iface
		interfaces []string
	)
	interfaces = append(interfaces, iface)

	if len(image) == 0 || len(sec) == 0 {
		return "", errors.New("BPF image or function is empty")
	}

	// Create an XdpProgram resource
	xdpProgram := &bpfdiov1alpha1.XdpProgram{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			//	Namespace: "bpfd", //TODO decide on namespace
			Labels: map[string]string{"afxdp.io/xdpprog": name},
		},
		Spec: bpfdiov1alpha1.XdpProgramSpec{
			BpfProgramCommon: bpfdiov1alpha1.BpfProgramCommon{
				BpfFunctionName: sec,
				NodeSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/hostname": node,
					},
				},
				ByteCode: bpfdiov1alpha1.BytecodeSelector{
					Image: &bpfdiov1alpha1.BytecodeImage{
						Url:             image,
						ImagePullPolicy: "IfNotPresent",
					},
				},
			},
			InterfaceSelector: bpfdiov1alpha1.InterfaceSelector{
				Interfaces: &interfaces,
			},
			Priority: 0,
		},
	}

	// Submit the XdpProgram resource to the API
	_, err := b.xdpProgramsClient.Create(context.TODO(), xdpProgram, metav1.CreateOptions{})
	if err != nil {
		return "", errors.Wrapf(err, "Failed to create XdpProgram resource: %v", err)
	}

	time.Sleep(time.Second)
	err = b.checkProgStatus(name)
	if err != nil {
		return "", errors.Wrapf(err, "Failed to create XdpProgram resource: %v", err)
	}

	bpfProgramList, err := b.GetBPFProgs()
	if err != nil {
		return "", errors.Wrapf(err, "Failed to get bpf prog resources: %v", err)
	}

	var xskmap string

	// Try to get the xspmap path from the bpf program resource
	if bpfProgramList != nil {
		bpfProgName := node + "-" + pm + "-" + iface + "-" + node + "-" + iface
		for _, bpfProgram := range bpfProgramList.Items {
			if bpfProgram.ObjectMeta.Name == bpfProgName {
				logging.Infof("FOUND bpfProgram %v", bpfProgram)
				if len(bpfProgram.Spec.Maps) == 0 {
					logging.Errorf("NO MAPS FOUND for %s", bpfProgName)
					return "", errors.New("Failed to find a map for the loaded bpf program")
				}
				for m, path := range bpfProgram.Spec.Maps {
					logging.Infof("map: %v", m)
					xskmap = path
				}

			}
		}
	}

	return xskmap, nil
}

// Delete XdpProgram
func (b *BpfdClient) DeleteXdpProg(iface string) error {

	pm := constants.Plugins.DevicePlugin.DevicePrefix
	name := nodeName + "-" + pm + "-" + iface

	// Delete the XdpProgram resource
	err := b.xdpProgramsClient.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		return errors.Wrapf(err, "Failed to delete XdpProgram resource: %v", err)
	}

	time.Sleep(time.Second)
	logging.Infof("Check resource status after deletion:\n%s\n", name)

	xdpProgramList, err := b.GetXdpProgs()
	if err != nil {
		return errors.Wrapf(err, "Failed to get xpd prog resources: %v", err)
	}

	// xdpProgramList can be nil if there are no xpd program resources
	if xdpProgramList != nil {
		for _, xdpProgram := range xdpProgramList.Items {
			if name == xdpProgram.ObjectMeta.Name {
				logging.Errorf("%s resource wasn't deleted\n", xdpProgram.ObjectMeta.Name)
				return errors.New("FAILED TO DELETE RESOURCE")
			}
		}
	}

	logging.Infof("Deleted XdpProgram resource:\n%s\n", name)

	return nil
}

func (b *BpfdClient) checkProgStatus(name string) error {

	selector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"afxdp.io/xdpprog": name,
		},
	}

	// Create a watcher to wait for a successful status
	xdpWatcher, err := b.xdpProgramsClient.Watch(context.TODO(),
		metav1.ListOptions{
			LabelSelector: labels.Set(selector.MatchLabels).String(),
			FieldSelector: fields.Set{"metadata.name": name}.AsSelector().String(),
			Watch:         true,
		})
	if err != nil {
		return errors.Wrapf(err, "Failed to watch XdpProgram resource: %v", err)
	}

	defer xdpWatcher.Stop()

	// Try to check status for 10 seconds
	for i := 0; i < 10; i++ {
		event, ok := <-xdpWatcher.ResultChan()
		if !ok {
			// channel closed
			logging.Errorf("Channel closed")
			return errors.New("Channel Closed")
		}

		prog, ok := event.Object.(*bpfdiov1alpha1.XdpProgram)
		if !ok {
			logging.Errorf("couldn't get xdp prog object")
			return errors.New("Failed to get xdp prog object")
		}

		// Get most recent condition
		recentIdx := len(prog.Status.Conditions) - 1
		condition := prog.Status.Conditions[recentIdx]

		switch condition.Type {
		case ProgramReconcileError:
			if i == 9 {
				logging.Errorf("Failed to load xdp program")
				return errors.New("Failed to load xdp program")
			}
		case BpfProgCondLoaded:
		case ProgramReconcileSuccess:
			logging.Infof("Bpf program Loaded %v", ProgramReconcileSuccess)
			return nil
		default:
			logging.Infof("Bpf program status %v", condition.Type)
		}
		time.Sleep(1 * time.Second)
	}

	return errors.New("Failed to load xdp program")
}
