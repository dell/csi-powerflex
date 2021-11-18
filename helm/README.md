# Dell EMC PowerFlex Helm Chart for Kubernetes

For detailed installation instructions, look in the product install [documention](https://dell.github.io/storage-plugin-docs/docs/installation/helm/powerflex/). Please make sure that you have ssh keys set up between the master and worker nodes, otherwise the install will break.

The general outline is:

    1. Satisfy the pre-requsites outlined in the product install documentation.

    2. Create a Kubernetes secret with the PowerFlex credentials using the template in secret.yaml.

    3. Copy the `csi-vxflexos/values.yaml` to a file  `csi-vxflexos/myvalues.yaml` in this directory and fill in various installation parameters.

    4. Run with the dry-run flag use this command line to confirm various parameters are as desired  
	   helm install --dry-run --values ./csi-vxflexos/myvalues.yaml --namespace vxflexos vxflexos ./csi-vxflexos"

       Run without the  "--dry-run" flag  to deploy the csi-driver

    4. Or use `csi-install.sh` shell script which deploys the helm chart for csi-vxflexos driver.
    
