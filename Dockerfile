# Copyright © 2019-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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
ARG GOIMAGE
ARG BASEIMAGE

# Stage to build the driver
FROM $GOIMAGE as builder
RUN mkdir -p /go/src
COPY ./ /go/src/

WORKDIR /go/src/
RUN make build

# Stage to build the driver image
FROM registry.access.redhat.com/ubi9/ubi@sha256:b07f5c75ad9669fa90a3aab2ccd4eb85d5a862265b09a695997ebe2be699bd20 AS final

# CVE remediation: update all packages to latest security fixes
# Addresses: CVE-2025-32462, CVE-2025-68973, CVE-2024-10963, CVE-2025-6965,
#            CVE-2026-3497, CVE-2024-12718, CVE-2025-4517, CVE-2026-28417, CVE-2026-33412
RUN dnf upgrade --security -y && \
    dnf clean all

# Explicit upgrades for available high CVE fixes:
# CVE-2026-34982 (vim-minimal) -> fixed in 2:8.2.2637-23.el9_7.3
# CVE-2026-4878  (libcap)      -> fixed in 2.48-10.el9_7.1
RUN dnf upgrade -y vim-minimal libcap && \
    dnf clean all

# Install required packages with security fixes
RUN dnf install -y \
    python3-3.9.* \
    openssh-8.* \
    gnupg2-2.3.* \
    sqlite-3.* \
    && dnf clean all

# Install packages available from standard UBI repositories
RUN dnf install -y \
    acl \
    bzip2 \
    gnutls \
    gzip \
    hostname \
    kmod \
    libaio \
    libuuid \
    libxcrypt-compat \
    nettle \
    numactl \
    openssl \
    rpm \
    systemd \
    tar \
    util-linux \
    which \
    && dnf clean all

# Install packages that are only available from full RHEL repositories.
# These packages (device-mapper-multipath, e2fsprogs, libblockdev, nfs-utils,
# nfs4-acl-tools, xfsprogs) are not shipped in any UBI repo.
# The Dell internal RHEL mirror is configured inline, used to install, then removed
# so no subscription artefacts remain in the final image layer.
RUN printf '[rhel-9-baseos]\nname=RHEL 9 BaseOS\nbaseurl=http://rhel-update.cec.lab.emc.com/rhel-9-for-x86_64-baseos-rpms\ngpgcheck=0\nsslverify=0\nenabled=1\n\n[rhel-9-appstream]\nname=RHEL 9 AppStream\nbaseurl=http://rhel-update.cec.lab.emc.com/rhel-9-for-x86_64-appstream-rpms\ngpgcheck=0\nsslverify=0\nenabled=1\n' > /etc/yum.repos.d/dell-rhel.repo && \
    dnf install -y \
    device-mapper-multipath \
    e2fsprogs \
    libblockdev \
    nfs-utils \
    nfs4-acl-tools \
    xfsprogs \
    && dnf clean all && \
    rm -f /etc/yum.repos.d/dell-rhel.repo

LABEL vendor="Dell Technologies" \
    maintainer="Dell Technologies" \
    name="csi-powerflex" \
    summary="CSI Driver for Dell EMC PowerFlex" \
    description="CSI Driver for provisioning persistent storage from Dell EMC PowerFlex" \
    release="1.15.2" \
    version="2.15.2" \
    license="Apache-2.0"

# copy in the driver
COPY --from=builder /go/src/csi-vxflexos /
COPY "csi-vxflexos.sh" /
RUN chmod +x /csi-vxflexos.sh
COPY ./licenses /licenses

ENTRYPOINT ["/csi-vxflexos.sh"]