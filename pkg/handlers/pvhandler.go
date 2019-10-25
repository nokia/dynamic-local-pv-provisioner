package handlers

import (
	"os"
	"log"
	"time"
	"reflect"
	"errors"
	syscall "golang.org/x/sys/unix"
	"github.com/nokia/dynamic-local-pv-provisioner/pkg/k8sclient"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/rest"
)

type PvHandler struct {
	nodeName 		string
	storagePath string
	k8sClient 	kubernetes.Interface
}

func NewPvHandler(storagePath string, cfg *rest.Config) (*PvHandler, error) {
	log.Println("DEBUG: NewPvHandler start...")
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	nodename := os.Getenv("NODE_NAME")
	pvHandler := PvHandler{
		nodeName :		nodename,
		storagePath: 	storagePath,
		k8sClient: 		kubeClient,
	}
	log.Println("DEBUG: pvHandler setted")
	lvCap, err := lvmAvailableCapacity(storagePath)
	if err != nil{
		return nil, err
	}
	log.Println("DEBUG: lvmCap determined")
	err = k8sclient.UpdateNodeLVCapacity(nodename, lvCap, kubeClient)
	log.Println("DEBUG: after patch")
	return &pvHandler, err
}

func (pvHandler *PvHandler) CreateController() cache.Controller {
	kubeInformerFactory := informers.NewSharedInformerFactory(pvHandler.k8sClient, time.Second*30)
	controller := kubeInformerFactory.Core().V1().PersistentVolumes().Informer()
	controller.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { pvHandler.pvAdded(*(reflect.ValueOf(obj).Interface().(*v1.PersistentVolume))) },
		DeleteFunc: func(obj interface{}) { pvHandler.pvDeleted(*(reflect.ValueOf(obj).Interface().(*v1.PersistentVolume))) },
		UpdateFunc: func(oldObj, newObj interface{}) {},
	})
	return controller
}

func (pvHandler *PvHandler) pvAdded(pv v1.PersistentVolume) {
	log.Printf("PV Added: %+v\n", pv)
	pvCapacity, ok := pv.Spec.Capacity["storage"]
	if !ok {
		log.Println("ERROR: pvAdded : no Capacity in PV")
	}
	lvmCap, err := lvmAvailableCapacity(pvHandler.storagePath)
	if err != nil{
		log.Println("ERROR: pvAdded - " + err.Error())
		return
	}
	calculatedCap := lvmCap - (&pvCapacity).Value()
	err = k8sclient.UpdateNodeLVCapacity(pvHandler.nodeName, calculatedCap, pvHandler.k8sClient)
	if err != nil{
		log.Println("ERROR: pvAdded - " + err.Error())
	}
}

func (pvHandler *PvHandler) pvDeleted(pv v1.PersistentVolume) {
	log.Printf("PV Deleted: %+v\n", pv)
	pvCapacity, ok := pv.Spec.Capacity["storage"]
	if !ok {
		log.Println("ERROR: pvDelete - no Capacity in PV")
	}
	lvmCap, err := lvmAvailableCapacity(pvHandler.storagePath)
	if err != nil{
		log.Println("ERROR: pvDelete - " + err.Error())
		return
	}
	calculatedCap := lvmCap + (&pvCapacity).Value()
	err = k8sclient.UpdateNodeLVCapacity(pvHandler.nodeName, calculatedCap, pvHandler.k8sClient)
	if err != nil{
		log.Println("ERROR: pvDelete - " + err.Error())
	}
}

func lvmAvailableCapacity (lvPath string) (int64, error) {
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(lvPath, &fs)
	if err != nil {
		return 0, errors.New("ERROR: Cannot get FS info from: " + lvPath + " because: " + err.Error())
	}
	return int64(fs.Bavail) * fs.Bsize, nil
}
