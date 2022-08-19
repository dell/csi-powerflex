# Copyright © 2019-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
# 
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License

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

# Adding RHEL8 files
RUN echo $'[rhel-8-baseos] \n\
name=Red Hat Enterprise Linux 8 (BaseOS) - $basearch \n\
baseurl=http://hb.us.dell.com/pub/redhat/RHEL8/stable/BaseOS/x86_64/os/ \n\
enabled=1 \n\
gpgcheck=1 \n\
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release,file:///etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-beta \n\
skip_if_unavailable=1' > /etc/yum.repos.d/dell-rpm.repo


RUN echo $'[rhel-8-appstream] \n\
name=Red Hat Enterprise Linux 8 (AppStream) - $basearch \n\
baseurl=http://hb.us.dell.com/pub/redhat/RHEL8/stable/AppStream/x86_64/os/ \n\
enabled=1 \n\
gpgcheck=1 \n\
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release,file:///etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-beta \n\
skip_if_unavailable=1' >> /etc/yum.repos.d/dell-rpm.repo

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
    version="2.5.0" \
    license="Apache-2.0"
COPY ./licenses /licenses



