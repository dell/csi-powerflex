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
# copy in the driver
COPY --from=builder /go/src/csi-vxflexos /
COPY "csi-vxflexos.sh" /
RUN chmod +x /csi-vxflexos.sh


# stage to run gosec
FROM builder as gosec
RUN go get github.com/securego/gosec/cmd/gosec
RUN cd /go/src && \
    gosec ./...
    
# Stage to check for critical and high CVE issues via Trivy (https://github.com/aquasecurity/trivy)
# will break image build if CRITICAL issues found
# will print out all HIGH issues found
FROM driver as cvescan
# run trivy and clean up all traces after
RUN curl https://raw.githubusercontent.com/aquasecurity/trivy/master/contrib/install.sh | sh && \
    trivy fs -s CRITICAL --exit-code 1 / && \
    trivy fs -s HIGH / && \
    trivy image --reset && \
    rm ./bin/trivy

# Stage to run antivirus scans via clamav (https://www.clamav.net/))
# will break image build if anything found
FROM driver as virusscan
# run trivy and clean up all traces after
RUN curl -o sqlite.rpm  http://mirror.centos.org/centos/8/BaseOS/x86_64/os/Packages/sqlite-libs-3.26.0-6.el8.x86_64.rpm && \
    rpm -iv  sqlite.rpm && \
    cd /etc/pki/ca-trust/source/anchors && curl -o dell.crt http://pki.dell.com/linux/dellca2018-bundle.crt && \
    curl -o emc.crt http://aia.dell.com/int/root/emcroot.crt && update-ca-trust && cd / && \ 
    yum install -y https://dl.fedoraproject.org/pub/epel/epel-release-latest-8.noarch.rpm && \
    yum install -y clamav clamav-update && \
    freshclam && \
    clamscan -r -i --exclude-dir=/sys / && \
    yum erase -y clamav clamav-update epel-release

# final stage
# simple stage to use the driver image as the resultant image 
FROM driver as final 
LABEL vendor="Dell Inc." \
      name="csi-powerflex" \
      summary="CSI Driver for Dell EMC PowerFlex" \
      description="CSI Driver for provisioning persistent storage from Dell EMC PowerFlex" \
      version="1.2.0" \
      license="Apache-2.0"
COPY ./licenses /licenses


