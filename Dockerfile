FROM centos
RUN yum install -y libaio
RUN yum install -y libuuid
RUN yum install -y numactl
RUN yum install -y xfsprogs
RUN yum install -y e4fsprogs
COPY "csi-vxflexos" .
ENTRYPOINT ["/csi-vxflexos"]
