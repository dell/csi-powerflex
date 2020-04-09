FROM centos:7.6.1810
RUN yum install -y libaio
RUN yum install -y libuuid
RUN yum install -y numactl
RUN yum install -y xfsprogs
RUN yum install -y e4fsprogs
COPY "csi-vxflexos" .
COPY "csi-vxflexos.sh" .
RUN chmod +x csi-vxflexos.sh
ENTRYPOINT ["/csi-vxflexos.sh"]
