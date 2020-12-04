# some arguments that must be supplied
ARG GOPROXY
ARG GOVERSION
ARG BASEIMAGE


# Stage to build the driver
FROM golang:${GOVERSION} as builder
ARG GOPROXY
RUN mkdir -p /go/src
COPY ./ /go/src/
WORKDIR /go/src/
RUN CGO_ENABLED=0 \
    make build

# stage to grab drv_cfg from the PowerFlex SDC package
FROM $BASEIMAGE as rpmgrabber
RUN dnf install -y \
    cpio \
    wget
# get SDC RPM file
RUN wget --no-check-certificate RPM_FILE_LINK
RUN rpm2cpio ./RPM_FILE_NAME i| cpio -idmv
RUN find /usr -name drv_cfg -exec cp -u {} /tmp \;

# Stage to build the driver image
FROM $BASEIMAGE AS driver
# install necessary packages
# alphabetical order for easier maintenance
RUN yum update -y && \
    yum install -y \
        e4fsprogs \
        kmod \
        libaio \
        libuuid \
        numactl \
        xfsprogs && \
    yum clean all && \
    rpm -e  --nodeps sqlite-libs
ENTRYPOINT ["/csi-vxflexos.sh"]
# copy in the drv_cfg
RUN mkdir -p /bin/emc/scaleio
COPY --from=rpmgrabber /tmp/drv_cfg /bin/emc/scaleio/drv_cfg
# copy in the driver
COPY --from=builder /go/src/csi-vxflexos /
COPY "csi-vxflexos.sh" /
RUN chmod +x /csi-vxflexos.sh

    
# final stage
# simple stage to use the driver image as the resultant image 
FROM driver as final

LABEL vendor="Dell Inc." \
      name="csi-powerflex" \
      summary="CSI Driver for Dell EMC PowerFlex" \
      description="CSI Driver for provisioning persistent storage from Dell EMC PowerFlex" \
      version="1.3.0" \
      license="Apache-2.0"
COPY ./licenses /licenses
