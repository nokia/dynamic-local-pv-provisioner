Overview
Dynamic local pv provisioner is az extension for kubernetes-sigs/sig-storage-local-static-provisioner, to provide a dynamic solution for Local Persistent Volume usage for Kubernetes.
It's capable of:
- dynamically allocate PVs to the required size
- pre-schedule the PV creation, based on the request in the annotation field


Components of the dynamic-local-pv-provisioner project
There 2 components in this project:
Provisioner:
The job of the provisioner is to determine the location of the PV, based on the annotation in the PVC and the available capacity.
If there's an avaialble node, it will update the PVC with the given nodename.
It will always try to fit the PV to a node where most capacity is available.
Only one instance is running from it

Executor:
The job of the executor:
- create bind mount as an input for kubernetes-sigs/sig-storage-local-static-provisioner
- update capacity info after PV is created 
Running as a DaemonSet

Usage
Dynamic local pv provisioner reads the annotaion field in the PVCs, so usage is via annotation.
Example1, "nodeSelector case":
   annotations:
       nokia.k8s.io/nodeSelector: '{"nodetype":"<NODES>"}'
With this annotation the PV will be scheduled to a "caas_master" node where the most capacity is available.
As a result a new annotation filed will be injected to the PVC:
   annotations:
       nokia.k8s.io/nodeSelector: '{"nodetype":"<NODES>"}'
	   nokia.k8s.io/nodeName: "<NODE1>"
So an executor wich runs on <NODE1> will create the bind mount input to kubernetes-sigs/sig-storage-local-static-provisioner which will create the PV on the given node

Example2, "nodeName case":
   annotations:
	   nokia.k8s.io/nodeName: "<NODE1>"
In this case the provisioner skips the PVC, executor creates the bind mount input to kubernetes-sigs/sig-storage-local-static-provisioner which will create the PV on the given node

Configuration
Executor needs to follwing volumes:
LVM from the mapper:
  - hostPath:
      path: "/dev/mapper/<LVM_NAME>"
      type: ""
    name: mapper
The directory where the LVM is mounted
  - hostPath:
      path: /mnt/<MOUNT_DIR_NAME>/
      type: ""
    name: sig-storage-mount
This is needed for the permanent bind mounts
  - hostPath:
      path: /etc/fstab
      type: ""
    name: fstab
These are needed for the permanent xfs quota setting
  - hostPath:
      path: /etc/projects
      type: FileOrCreate
    name: projects
  - hostPath:
      path: /etc/projid
      type: FileOrCreate
    name: projid



Build


Installation

