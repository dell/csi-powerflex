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

# Stage to build the driver image
FROM $BASEIMAGE AS driver
# install necessary packages
# alphabetical order for easier maintenance
RUN microdnf update -y && \
    microdnf install -y \
        e4fsprogs \
        kmod \
        libaio \
        numactl \
        xfsprogs && \
    microdnf clean all 
ENTRYPOINT ["/csi-vxflexos.sh"]
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
    version="1.5.0" \
    license="Apache-2.0"
COPY ./licenses /licenses
