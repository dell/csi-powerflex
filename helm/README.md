# Dell EMC PowerFlex Helm Chart for Kubernetes

For detailed installation instructions, look in the product install [documention](https://dell.github.io/csm-docs/docs/csidriver/installation/helm/powerflex/)

The general outline is:

    1. Satisfy the pre-requsites outlined in the product install documentation.

    2. Create a Kubernetes secret with the PowerFlex credentials using the template in `../samples/config.yaml`.

    3. Copy `csi-vxflexos/values.yaml` to a file called `csi-vxflexos/myvalues.yaml` in this directory and fill in various installation parameters.

    4. Run the helm install command, first using the dry-run flag to confirm various parameters are as desired.  
	   helm install --dry-run --values ./csi-vxflexos/myvalues.yaml --namespace vxflexos vxflexos ./csi-vxflexos"

       Then, run without the  "--dry-run" flag  to deploy the csi-driver.

    4. Or use `csi-install.sh` shell script which deploys the helm chart for csi-vxflexos driver.
    
