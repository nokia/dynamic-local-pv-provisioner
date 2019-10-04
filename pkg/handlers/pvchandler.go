package handlers

import (
	"os"
	"os/exec"
	"log"
	"strings"
	"strconv"
	"fmt"
	"time"
	"reflect"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/rest"
	syscall "golang.org/x/sys/unix"
)

const (
	fstabPath = "/rootfs/fstab"
)

type PvcHandler struct {
	storagePath string
	k8sClient 	kubernetes.Interface
}

func NewPvcHandler(storagePath string, cfg *rest.Config) (*PvcHandler, error) {
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	pvcHandler := PvcHandler{
		storagePath: storagePath,
		k8sClient: kubeClient,
	}
	return &pvcHandler, nil
}

func (pvcHandler *PvcHandler) CreateController() cache.Controller {
	kubeInformerFactory := informers.NewSharedInformerFactory(pvcHandler.k8sClient, time.Second*30)
	controller := kubeInformerFactory.Core().V1().PersistentVolumeClaims().Informer()
	controller.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { pvcHandler.pvcAdded(*(reflect.ValueOf(obj).Interface().(*v1.PersistentVolumeClaim))) },
		DeleteFunc: func(obj interface{}) {},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pvcHandler.pvcChanged(*(reflect.ValueOf(oldObj).Interface().(*v1.PersistentVolumeClaim)), *(reflect.ValueOf(newObj).Interface().(*v1.PersistentVolumeClaim)))
		},
	})
	return controller
}

func (pvcHandler *PvcHandler) pvcAdded(pvc v1.PersistentVolumeClaim) {
	log.Printf("DEBUG: PVC Added: %+v\n", pvc)
	handlePvc, pvDirPath := shouldPvcBeHandled(pvc, pvcHandler.storagePath)
	if !handlePvc {
		log.Println("DEBUG: pvcAdded - Not my job...")
		return
	}
	pvcHandler.createPVStorage(pvc, pvDirPath)
}

func (pvcHandler *PvcHandler) pvcChanged(oldPvc v1.PersistentVolumeClaim, newPvc v1.PersistentVolumeClaim) {
	log.Printf("PVC changed: %+v\n", newPvc)
	handlePvc, pvDirPath := shouldPvcBeHandled(newPvc, pvcHandler.storagePath)
	if !handlePvc {
		log.Println("DEBUG: pvcChanged - Not my job...")
		return
	}
	pvcHandler.createPVStorage(newPvc, pvDirPath)
}

func shouldPvcBeHandled(pvc v1.PersistentVolumeClaim, storagePath string) (bool, string) {
	if pvc.Status.Phase == v1.ClaimPending {
		if nodeName, ok := pvc.ObjectMeta.Annotations["nodename"]; ok && (nodeName == os.Getenv("NODE_NAME")) {
			pvDir := storagePath + "/" + pvc.ObjectMeta.Namespace + "_" + pvc.ObjectMeta.Name
			if _, err := os.Stat(pvDir); os.IsNotExist(err) {
				return true, pvDir
			}
			log.Println("DEBUG: " + pvDir + " already exists!")
		} else {
			log.Print("DEBUG: Nodename: %t, %s, env: %s\n", ok, nodeName, os.Getenv("NODE_NAME"))
		}
	}
	return false, ""
}

func (pvcHandler *PvcHandler) createPVStorage(pvc v1.PersistentVolumeClaim, pvDirPath string) {
	var projectLines []string

	log.Println("DEBUG: Starting createPVStorage executor...")
	pvcStorageReq, ok := pvc.Spec.Resources.Requests["storage"]
	if !ok {
		log.Println("ERROR: Storage request is empty!")
		return
	}
	log.Printf("DEBUG: storage resource = %v\n", pvcStorageReq)
	log.Printf("DEBUG: storage resource value = %v\n", (&pvcStorageReq).Value())
	storageRequest := strconv.FormatInt((&pvcStorageReq).Value(), 10)
	log.Println("DEBUG: storageRequest: " + storageRequest)

	command := exec.Command("xfs_quota", "-x", "-c", "report", pvcHandler.storagePath)
	output, err := command.Output()
	if err != nil {
		log.Println("ERROR: Cannot get XFS quota reports, because: " + err.Error())
		return
	}

	lines := strings.Split(string(output),"\n")
	for _, line := range lines {
		if strings.HasPrefix(line,"#") {
			projectLines = append(projectLines, line)
		}
	}
	lastline := string(projectLines[len(projectLines)-1])
	projID, err := strconv.Atoi(strings.TrimPrefix(strings.Split(lastline, " ")[0], "#"))
	if err != nil{
		log.Println("ERROR: Cannot convert project id from " + lastline + " because: " + err.Error())
		return
	}
	// create directory with new projID
	projID = projID + 1
	err = os.Mkdir(pvDirPath, os.ModePerm)
	if err != nil {
		log.Println("ERROR: Cannot create directory on host, because: " + err.Error())
		return
	}
	// set xfs_quota limit
	subcommand := fmt.Sprintf("'project -s -p %s %d'", pvDirPath, projID)
	command = exec.Command("xfs_quota", "-x", "-c", subcommand, pvcHandler.storagePath)
	_, err = command.Output()
	if err != nil {
		log.Println("ERROR: Cannot set xfs_quota project, because: " + err.Error())
		return
	}

	subcommand = fmt.Sprintf("'limit -p bhard=%s %d'", storageRequest, projID)
	command = exec.Command("xfs_quota", "-x", "-c", subcommand, pvcHandler.storagePath)
	_, err = command.Output()
	if err != nil {
		log.Println("ERROR: Cannot set xfs_quota limit, because: " + err.Error())
		return
	}

	log.Println("DEBUG: Bind Mount... ")
	// Bind mounting
	err = syscall.Mount(pvDirPath, pvDirPath, "none", syscall.MS_BIND, "")
	if err != nil {
		log.Println("ERROR: bind mount directories, because: " + err.Error())
		return
	}
	// Set fstab file
	file, err := os.OpenFile(fstabPath, os.O_APPEND|os.O_WRONLY|os.O_SYNC, 0755)
	if err != nil {
		log.Println("ERROR: Cannot open fstab file: " + fstabPath + " because: " + err.Error()+ "\nCannot save mountpoint!")
		return
	}
	defer file.Close()
	bindMountCommand := fmt.Sprintf("%[1]s %[1]s none bind 0 0", pvDirPath)
	_,err = file.WriteString(bindMountCommand)
	if err != nil {
		log.Println("ERROR: Cannot modify fstab file: " + fstabPath + " because: " + err.Error()+ "\nCannot save mountpoint!")
		return
	}
	log.Println("DEBUG: createPVStorage executor successfull!")
}
