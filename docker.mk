# docker makefile, included from Makefile, will build/push images with docker or podman
#

# Includes the following generated file to get semantic version information
include semver.mk

ifdef NOTES
	RELNOTE="-$(NOTES)"
else
	RELNOTE=
endif

ifeq ($(IMAGETAG),)
IMAGETAG="v$(MAJOR).$(MINOR).$(PATCH)$(RELNOTE)"
endif


docker: download-csm-common
	$(eval include csm-common.mk)
	@echo "Base Images is set to: $(BASEIMAGE)"
	@echo "Building: $(REGISTRY)/$(IMAGENAME):$(IMAGETAG)"
	$(BUILDER) build $(NOCACHE) -t "$(REGISTRY)/$(IMAGENAME):$(IMAGETAG)" --target $(BUILDSTAGE) --build-arg GOPROXY --build-arg BASEIMAGE=$(CSM_BASEIMAGE) --build-arg GOIMAGE=$(DEFAULT_GOIMAGE)  .

docker-no-cache: download-csm-common
	@echo "Building with --no-cache ..."
	@make docker NOCACHE=--no-cache

push:
	@echo "Pushing: $(REGISTRY)/$(IMAGENAME):$(IMAGETAG)"
	$(BUILDER) push "$(REGISTRY)/$(IMAGENAME):$(IMAGETAG)"


download-csm-common:
	curl -O -L https://raw.githubusercontent.com/dell/csm/main/config/csm-common.mk
