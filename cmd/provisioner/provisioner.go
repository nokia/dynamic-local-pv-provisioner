package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"time"
	"reflect"
	syscall "golang.org/x/sys/unix"
	"github.com/nokia/dynamic-local-pv-provisioner/pkg/k8sclient"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

// const ()
//
var (
	kubeConfig 	string
	// storagePath string
)

type Provisoner struct {
	k8sClient 	kubernetes.Interface
}

func main() {
	flag.Parse()
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		log.Fatal("ERROR: Parsing kubeconfig failed with error: " + err.Error() + ", exiting!")
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatal("ERROR: Get k8s client: " + err.Error())
	}
	provisioner := Provisoner{
		k8sClient : client,
	}
	kubeInformerFactory := informers.NewSharedInformerFactory(provisioner.k8sClient, time.Second*30)
	provisonerController := kubeInformerFactory.Core().V1().PersistentVolumeClaims().Informer()
	provisonerController.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { provisioner.pvcAdded(*(reflect.ValueOf(obj).Interface().(*v1.PersistentVolumeClaim))) },
		DeleteFunc: func(obj interface{}) {},
		UpdateFunc: func(oldObj, newObj interface{}) {
			provisioner.pvcChanged(*(reflect.ValueOf(oldObj).Interface().(*v1.PersistentVolumeClaim)), *(reflect.ValueOf(newObj).Interface().(*v1.PersistentVolumeClaim)))
		},
	})
	stopChannel := make(chan struct{})
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
	log.Println("Storage Provisoner controller initalized successfully! Warm-up starts now!")

	go provisonerController.Run(stopChannel)
	// Wait until Controller pushes a signal on the stop channel
	select {
	case <-stopChannel:
		log.Fatal("Storage Provisoner controller stopped abruptly, exiting!")
	case <-signalChannel:
		log.Println("Orchestrator initiated graceful shutdown. See you soon!")
	}
}

func (provisioner *Provisoner) pvcAdded(pvc v1.PersistentVolumeClaim) {
	log.Printf("DEBUG: PROVISIONER PVC Added: %+v\n", pvc)
	selector, selectorOk := pvc.ObjectMeta.Annotations["nodeselector"]
	_, nodeNameOk := pvc.ObjectMeta.Annotations["nodename"]
	if selectorOk && !nodeNameOk && (pvc.Status.Phase != v1.ClaimBound) {
		log.Println("DEBUG: Selector: " + selector)
		node, err := k8sclient.GetNodeByLabel(selector, provisioner.k8sClient)
		log.Printf("DEBUG: Choosen node: %+v\n", node)
		if err != nil {
			log.Println("ERROR: Cannot query node by label, because: " + err.Error())
			return
		}
		nodeCapacity := node.Status.Capacity["lv-capacity"]
		if (&nodeCapacity).Cmp(pvc.Spec.Resources.Requests["storage"]) < 0 {
			log.Println("ERROR: Not enough free space in storage!")
			return
		}
		pvc.ObjectMeta.Annotations["nodename"] = node.ObjectMeta.Name
		// test if could be updated
		pvc.ObjectMeta.ResourceVersion = ""
		log.Printf("DEBUG: Pvc before update: %+v\n", pvc)
		err = k8sclient.UpdatePvc(pvc, provisioner.k8sClient)
		if err != nil {
			log.Println("ERROR: Cannot update PVC, because: " + err.Error())
		}
	} else {
		log.Println("DEBUG: PROVISIONER pvcAdded - Not my job...")
	}
}

func (provisioner *Provisoner) pvcChanged(oldPvc v1.PersistentVolumeClaim, newPvc v1.PersistentVolumeClaim) {
	// needed???
}

func init() {
	// flag.StringVar(&storagePath, "storagepath", "", "The path where VG is mounted and where sig-storage-controller is watching. Mandatory parameter.")
	flag.StringVar(&kubeConfig, "kubeconfig", "", "Path to a kubeconfig. Optional parameter, only required if out-of-cluster.")
}
