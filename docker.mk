# Includes the following generated file to get semantic version information
include semver.mk
ifdef NOTES
	RELNOTE="-$(NOTES)"
else
	RELNOTE=
endif

docker:
	echo "MAJOR $(MAJOR) MINOR $(MINOR) PATCH $(PATCH) RELNOTE $(RELNOTE) SEMVER $(SEMVER)"
	docker build -t "artifactory-sio.isus.emc.com:8129/csi-vxflexos:v$(MAJOR).$(MINOR).$(PATCH)$(RELNOTE)" .

push:   
	echo "MAJOR $(MAJOR) MINOR $(MINOR) PATCH $(PATCH) RELNOTE $(RELNOTE) SEMVER $(SEMVER)"
	docker push "artifactory-sio.isus.emc.com:8129/csi-vxflexos:v$(MAJOR).$(MINOR).$(PATCH)$(RELNOTE)"
