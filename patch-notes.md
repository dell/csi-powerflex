# CSI Driver for PowerFlex Patches

## Patch 1.2.1
##### Image:    
dellemc/csi-vxflexos:v1.2.1
##### Updates:      
This patch updates the base image to UBI 8, fixing CVEs that were reported with UBI 7 base image. 
##### Instructions to use: 
###### Helm:  
New driver image is already supplied in helm/csi-vxflexos/driver-image.yaml, run:  
` ./csi-install.sh --namespace vxflexos --values <your values file>  --upgrade`    
to upgrade your driver to the patched image. 

###### Operator:
Replace image dellemc/csi-vxflexos:v1.2.0.000R with dellemc/csi-vxflexos:v1.2.1 in the following yamls:  

Upstream Kubernetes:  
samples/vxflex_120_k8s_117.yaml  
samples/vxflex_120_k8s_118.yaml  
samples/vxflex_120_k8s_119.yaml    

Openshift Environments:   
samples/vxflex_120_ops_43.yaml  
samples/vxflex_120_ops_44.yaml  
