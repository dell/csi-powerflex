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


docker:
	@echo "Base Images is set to: $(BASEIMAGE)"
	@echo "Building: $(REGISTRY)/$(IMAGENAME):$(IMAGETAG)"
	$(BUILDER) build -t "$(REGISTRY)/$(IMAGENAME):$(IMAGETAG)" --target $(BUILDSTAGE) --build-arg GOPROXY --build-arg BASEIMAGE=$(BASEIMAGE) --build-arg GOVERSION=$(GOVERSION) --build-arg DIGEST=$(DIGEST) .

push:   
	@echo "Pushing: $(REGISTRY)/$(IMAGENAME):$(IMAGETAG)"
	$(BUILDER) push "$(REGISTRY)/$(IMAGENAME):$(IMAGETAG)"
