package k8sclient

import (
	"log"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/api/core/v1"
)

func GetNodeByLabel(label string, kubeClient kubernetes.Interface) (v1.Node, error) {
	var returnNode v1.Node
	var maxCapacity int64 = 0
	var listOption metav1.ListOptions

	if label != "" {
		listOption = metav1.ListOptions{LabelSelector: label}
	}
	nodeList, err := kubeClient.CoreV1().Nodes().List(listOption)
	log.Printf("DEBUG: result node list: %+v\n", nodeList)
	if err != nil {
		return v1.Node{}, err
	}
	switch nodesLen := len(nodeList.Items); nodesLen {
	case 0:
		return v1.Node{}, errors.New("ERROR: No nodes found for label:" + label + "!")
	case 1:
		return nodeList.Items[0], nil
	default:
		for _, node := range nodeList.Items {
			nodeCapacity := node.Status.Capacity["lv-capacity"]
			if (&nodeCapacity).CmpInt64(maxCapacity) == 1 {
				maxCapacity = (&nodeCapacity).Value()
				returnNode = node
			}
		}
	}
	return returnNode, nil
}

func UpdatePvc(pvc v1.PersistentVolumeClaim, kubeClient kubernetes.Interface) error {
	log.Printf("DEBUG: UpdatePvc PVC for update: %+v\n", pvc)
	result, err := kubeClient.CoreV1().PersistentVolumeClaims(pvc.ObjectMeta.Namespace).Update(&pvc)
	if err != nil {
		return errors.New("ERROR: Cannot update pvc because: " + err.Error())
	}
	log.Printf("DEBUG: UpdatePvc result:  %+v\n", result)
	return nil
}

func GetNode(nodeName string, kubeClient kubernetes.Interface) (*v1.Node, error) {
	node, err := kubeClient.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
 	if err != nil {
		return nil, errors.New("ERROR: Cannot get node (" + nodeName + ") because: " + err.Error())
	}
	return node, nil
}

func UpdateNodeStatus(nodeName string, kubeClient kubernetes.Interface, node *v1.Node) error {
	// node, err := kubeClient.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
 	// if err != nil {
	// 	return errors.New("ERROR: Cannot get lv-capacity on node because: " + err.Error())
	// }
	// node.Status.Capacity["lv-capacity"] = lvCapQuantity
	updatenode, err := kubeClient.CoreV1().Nodes().UpdateStatus(node)
	if err != nil {
		return errors.New("ERROR: Cannot update node staus of node (" + nodeName + ") because: " + err.Error())
	}
	log.Printf("DEBUG: UpdateNodeStatus: %+v\n", updatenode)
	return nil
}
