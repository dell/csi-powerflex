# Dell EMC PowerFlex Helm Chart for Kubernetes

For detailed installation instructions, look in the product install [documention](https://dell.github.io/storage-plugin-docs/docs/installation/helm/powerflex/)

The general outline is:

    1. Satisfy the pre-requsites outlined in the product install documentation.

    2. Create a Kubernetes secret with the PowerFlex credentials using the template in secret.yaml.

    3. Copy the `csi-vxflexos/values.yaml` to a file  `myvalues.yaml` in this directory and fill in various installation parameters.

    4. Invoke the `install.vxflexos` shell script which deploys the helm chart in csi-vxflexos.
    
