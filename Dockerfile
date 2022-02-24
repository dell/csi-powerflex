# some arguments that must be supplied
ARG GOPROXY
ARG GOVERSION
ARG BASEIMAGE
ARG DIGEST

# Stage to build the driver
FROM golang:${GOVERSION} as builder
ARG GOPROXY
RUN mkdir -p /go/src
COPY ./ /go/src/
WORKDIR /go/src/
RUN CGO_ENABLED=0 \
    make build

# Stage to build the driver image
FROM $BASEIMAGE@${DIGEST} AS final
# install necessary packages
# alphabetical order for easier maintenance
RUN microdnf update -y && \
    microdnf install -y  \
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
LABEL vendor="Dell Inc." \
    name="csi-powerflex" \
    summary="CSI Driver for Dell EMC PowerFlex" \
    description="CSI Driver for provisioning persistent storage from Dell EMC PowerFlex" \
    version="2.1.0" \
    license="Apache-2.0"
COPY ./licenses /licenses



