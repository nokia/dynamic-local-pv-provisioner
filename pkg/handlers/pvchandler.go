package handlers

import (
	"os/exec"
	"os"
	"log"
	"strings"
	"strconv"
	"fmt"
	"time"
	"reflect"
	"errors"

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
	if nodeName, ok := pvc.ObjectMeta.Annotations["nodename"]; (pvc.Status.Phase != v1.ClaimBound) && ok && (nodeName == os.Getenv("NODE_NAME")) {
		log.Println("DEBUG: PVC handle")
		pvcStorageReq := pvc.Spec.Resources.Requests["storage"]
		err := pvcHandler.createPVStorage(strconv.FormatInt((&pvcStorageReq).Value(), 10))
		if err != nil{
			log.Println("ERROR: PVC-Added failed: " + err.Error())
		}
	}else{
		log.Println("DEBUG: pvcAdded - Not my job...")
	}
}

func (pvcHandler *PvcHandler) pvcChanged(oldPvc v1.PersistentVolumeClaim, newPvc v1.PersistentVolumeClaim) {
	log.Printf("PVC changed: %+v\n", newPvc)
	_, oldOk:= oldPvc.Spec.Resources.Requests["storage"];
	pvcStorageReq, newOk := newPvc.Spec.Resources.Requests["storage"]
	if (newPvc.Status.Phase != v1.ClaimBound) && !oldOk && newOk{
		err := pvcHandler.createPVStorage(strconv.FormatInt((&pvcStorageReq).Value(), 10))
		if err != nil{
			log.Println("ERROR: PVC-Changed failed: " + err.Error())
		}
	}else{
		log.Println("DEBUG: pvcChanged - Not my job...")
	}
}

func runOSCommand(command string) (string, error) {
	log.Println("DEBUG: " + command)
	out, err := exec.Command("sh", "-c", command).Output()
	if err != nil {
		return "", err
	}
	output := string(out[:])
	log.Println("DEBUG: output: " + output)
	return output, nil
}

func (pvcHandler *PvcHandler) createPVStorage(storageRequest string) error{
	log.Println("DEBUG: Starting createPVStorage executor...")
	if storageRequest == ""{
		return errors.New("Error: Storage request is empty...")
	}
	// get xfs_quota last proj_id
	command := "xfs_quota -x -c report " + pvcHandler.storagePath + " | grep '#'"
	log.Println(command)
	output, err := runOSCommand(command)
	if err != nil {
		return errors.New("ERROR: Cannot get XFS quota reports: " + err.Error())
	}
	lines := strings.Split(output,"\n")
	lastline := lines[len(lines)-2]
	projID, err := strconv.Atoi(strings.TrimPrefix(strings.Split(lastline," ")[0],"#"))
	if err != nil{
		return errors.New("ERROR: Cannot convert project id from " + lastline + " because:" + err.Error())
	}
	// create directory with new projID
	projID = projID + 1
	pvDirPath := fmt.Sprintf("%s/%d", pvcHandler.storagePath, projID)
	err = os.Mkdir(pvDirPath, os.ModePerm)
	if err != nil {
		return errors.New("ERROR: Cannot create directory: " + err.Error())
	}
	// set xfs_quota limit
	command = fmt.Sprintf("xfs_quota -x -c 'project -s -p %s %d' %s", pvDirPath, projID, pvcHandler.storagePath)
	_, err = runOSCommand(command)
	if err != nil {
		return errors.New("ERROR: Cannot set xfs_quota project:" + err.Error())
	}
	command = fmt.Sprintf("xfs_quota -x -c 'limit -p bhard=%s %d' %s", storageRequest, projID, pvcHandler.storagePath)
	log.Println(command)
	_, err = runOSCommand(command)
	if err != nil {
		return errors.New("ERROR: Cannot set xfs_quota limit:" + err.Error())
	}

	log.Println("DEBUG: Bind Mount... ")
	// err = mount.MakeMount(pvDirPath)
	err = syscall.Mount(pvDirPath, pvDirPath, "none", syscall.MS_BIND, "")
	if err != nil {
		return errors.New("Error bind mount directories: " + err.Error())
	}
	// Set fstab file
	file, err := os.OpenFile(fstabPath, os.O_WRONLY|os.O_SYNC, 0755)
	if err != nil {
		return errors.New("Can't open fstab file: " + fstabPath + " because:" + err.Error() + "\nCannot save mountpoint!")
	}
	defer file.Close()
	bindMountCommand := fmt.Sprintf("%[1]s %[1]s none bind 0 0", pvDirPath)
	_,err = file.WriteString(bindMountCommand)
	if err != nil {
		return errors.New("Can't modify fstab file: " + fstabPath + " because:" + err.Error() + "\nCannot save mountpoint!")
	}
	log.Println("DEBUG: createPVStorage executor successfull!")
	return nil
}
