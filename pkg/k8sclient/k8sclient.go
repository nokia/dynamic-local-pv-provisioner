package k8sclient

import (
	"log"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/api/core/v1"
)

const (
	NodeSelector = "nokia.k8s.io/nodeselector"
	NodeName = "nokia.k8s.io/nodename"
	LvCapacity = "nokia.k8s.io/lv-capacity"
)

func GetNodeByLabel(label string, kubeClient kubernetes.Interface) (v1.Node, error) {
	var returnNode v1.Node
	var maxCapacity int64 = 0
	var listOption metav1.ListOptions

	// if label != "" {
	listOption = metav1.ListOptions{LabelSelector: label}
	// }
	nodeList, err := kubeClient.CoreV1().Nodes().List(listOption)
	log.Printf("DEBUG: result node list: %+v\n", nodeList)
	if err != nil {
		return v1.Node{}, err
	}
	switch nodesLen := len(nodeList.Items); nodesLen {
	case 0:
		return v1.Node{}, errors.New("No nodes found for label:" + label + "!")
	case 1:
		return nodeList.Items[0], nil
	default:
		for _, node := range nodeList.Items {
			nodeCapacity, ok := node.Status.Capacity[LvCapacity]
			if !ok {
				return returnNode, errors.New("No lv-capacity set, yet!")
			}
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
		return err
	}
	log.Printf("DEBUG: UpdatePvc result:  %+v\n", result)
	return nil
}

func GetNode(nodeName string, kubeClient kubernetes.Interface) (*v1.Node, error) {
	node, err := kubeClient.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
 	if err != nil {
		return nil, err
	}
	return node, nil
}

func UpdateNodeStatus(nodeName string, kubeClient kubernetes.Interface, node *v1.Node) error {
	updatenode, err := kubeClient.CoreV1().Nodes().UpdateStatus(node)
	if err != nil {
		return err
	}
	log.Printf("DEBUG: UpdateNodeStatus: %+v\n", updatenode)
	return nil
}
