#Use rocky Linux as the base image
FROM rockylinux/rockylinux:8.4
# Copy the files from the host to the container
COPY "csi-vxflexos" .
COPY "csi-vxflexos.sh" .
COPY "scripts/mkraid.disk.sh" .
COPY "scripts/get.pflexvol.sh" .
COPY "scripts/get.emcvol.sh" .
COPY "scripts/init.node.sh" .

RUN yum install -y \
    e2fsprogs \
    which \
    xfsprogs \
    device-mapper-multipath \
    libaio \
    mdadm \
    numactl \
    libuuid \
    e4fsprogs \
    nfs-utils \
    procps-ng \
    && \
    yum clean all \
    && \
    rm -rf /var/cache/run

# validate some cli utilities are found
RUN which mkfs.ext4
RUN which mkfs.xfs

# Set the command to run when the container starts
ENTRYPOINT ["/csi-vxflexos.sh"]
