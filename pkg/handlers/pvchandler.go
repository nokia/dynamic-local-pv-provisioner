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
	"path/filepath"
	"io/ioutil"
	"github.com/nokia/dynamic-local-pv-provisioner/pkg/k8sclient"

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
	if !pvcHandler.enoughLvCapacity(pvc) {
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
	if !pvcHandler.enoughLvCapacity(newPvc) {
		log.Println("ERROR: Not enough storage!")
		return
	}
	pvcHandler.createPVStorage(newPvc, pvDirPath)
}

func (pvcHandler *PvcHandler) enoughLvCapacity(pvc v1.PersistentVolumeClaim) bool {
	node, err := k8sclient.GetNode(os.Getenv("NODE_NAME"), pvcHandler.k8sClient)
	if err != nil {
		log.Println("ERROR: Not enough free space in storage!")
		return false
	}
	nodeCapacity := node.Status.Capacity["lv-capacity"]
	if (&nodeCapacity).Cmp(pvc.Spec.Resources.Requests["storage"]) < 0 {
		log.Println("ERROR: Not enough free space in storage!")
		return false
	}
	return true
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
			log.Printf("DEBUG: Nodename: %t, %s, env: %s\n", ok, nodeName, os.Getenv("NODE_NAME"))
		}
	}
	return false, ""
}

func (pvcHandler *PvcHandler) createPVStorage(pvc v1.PersistentVolumeClaim, pvDirPath string) {
	var projID int = 1

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

	projectsContent, err := ioutil.ReadFile("/etc/projects")
	if err != nil {
		log.Println("ERROR: Cannot read /etc/projects file: " + err.Error())
		return
	}
	if string(projectsContent) != "" {
		lines := strings.Split(string(projectsContent),"\n")
		projid, err := strconv.Atoi(strings.Split(lines[len(lines)-2], ":")[0])
		if err != nil{
			log.Println("ERROR: Cannot convert project id from " + lines[len(lines)-2] + " because: " + err.Error())
			return
		}
		projID = projid + 1
	}
	// create directory with new projID
	err = os.Mkdir(pvDirPath, os.ModePerm)
	if err != nil {
		log.Println("ERROR: Cannot create directory on host, because: " + err.Error())
		return
	}

	projFile, err := os.OpenFile("/etc/projects", os.O_APPEND|os.O_WRONLY|os.O_SYNC, 0755)
	if err != nil {
		log.Println("ERROR: Cannot open /etc/projects file, because: " + err.Error())
		return
	}
	defer projFile.Close()
	project := fmt.Sprintf("%d:%s\n", projID, pvDirPath)
	_,err = projFile.WriteString(project)
	if err != nil {
		log.Println("ERROR: Cannot modify /etc/projects file, because: " + err.Error())
		return
	}
	projIdFile, err := os.OpenFile("/etc/projid", os.O_APPEND|os.O_WRONLY|os.O_SYNC, 0755)
	if err != nil {
		log.Println("ERROR: Cannot open /etc/projid file, because: " + err.Error())
		return
	}
	defer projIdFile.Close()
	projName := filepath.Base(pvDirPath)
	projid := fmt.Sprintf("%s:%d\n", projName, projID)
	_,err = projIdFile.WriteString(projid)
	if err != nil {
		log.Println("ERROR: Cannot modify /etc/projid file, because: " + err.Error())
		return
	}
	// set xfs_quota limit
	subcommand := fmt.Sprintf("project -s %s", projName)
	command := exec.Command("xfs_quota", "-x", "-c", subcommand, pvcHandler.storagePath)
	output, err := command.CombinedOutput()
	if err != nil {
		log.Println("ERROR: Cannot set xfs_quota project, because: " + err.Error())
		return
	}
	log.Printf("DEBUG: command: %+v\n", command)
	log.Println("DEBUG: output: " + string(output))

	subcommand = fmt.Sprintf("limit -p bhard=%s %s", storageRequest, projName)
	command = exec.Command("xfs_quota", "-x", "-c", subcommand, pvcHandler.storagePath)
	output, err = command.CombinedOutput()
	if err != nil {
		log.Println("ERROR: Cannot set xfs_quota limit, because: " + err.Error())
		return
	}
	log.Printf("DEBUG: command: %+v\n", command)
	log.Println("DEBUG: output: " + string(output))
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
	bindMountCommand := fmt.Sprintf("%[1]s %[1]s none bind 0 0\n", pvDirPath)
	_,err = file.WriteString(bindMountCommand)
	if err != nil {
		log.Println("ERROR: Cannot modify fstab file: " + fstabPath + " because: " + err.Error()+ "\nCannot save mountpoint!")
		return
	}
	log.Println("DEBUG: createPVStorage executor successfull!")
}
