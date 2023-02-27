# overrides file
# this file, included from the Makefile, will overlay default values with environment variables
#

# DEFAULT values
DEFAULT_BASEIMAGE="registry.access.redhat.com/ubi8/ubi-minimal"
# digest for  8.7-1049.1675784874
DEFAULT_DIGEST="sha256:0214a28336e387c66493c61bb394e86a18f3bea8dbc46de74a26f173ff553c89"
DEFAULT_GOVERSION="1.20"
DEFAULT_REGISTRY="sample_registry"
DEFAULT_IMAGENAME="csi-vxflexos"
DEFAULT_BUILDSTAGE="final"
DEFAULT_IMAGETAG=""

# set the BASEIMAGE if needed
ifeq ($(BASEIMAGE),)
export BASEIMAGE="$(DEFAULT_BASEIMAGE)"
endif

# set the IMAGEDIGEST if needed
ifeq ($(DIGEST),)
export DIGEST="$(DEFAULT_DIGEST)"
endif

# set the GOVERSION if needed
ifeq ($(GOVERSION),)
export GOVERSION="$(DEFAULT_GOVERSION)"
endif

# set the REGISTRY if needed
ifeq ($(REGISTRY),)
export REGISTRY="$(DEFAULT_REGISTRY)"
endif

# set the IMAGENAME if needed
ifeq ($(IMAGENAME),)
export IMAGENAME="$(DEFAULT_IMAGENAME)"
endif

#set the IMAGETAG if needed
ifneq ($(DEFAULT_IMAGETAG), "") 
export IMAGETAG="$(DEFAULT_IMAGETAG)"
endif

# set the BUILDSTAGE if needed
ifeq ($(BUILDSTAGE),)
export BUILDSTAGE="$(DEFAULT_BUILDSTAGE)"
endif

# figure out if podman or docker should be used (use podman if found)
ifneq (, $(shell which podman 2>/dev/null))
export BUILDER=podman
else
export BUILDER=docker
endif

# target to print some help regarding these overrides and how to use them
overrides-help:
	@echo
	@echo "The following environment variables can be set to control the build"
	@echo
	@echo "GOVERSION   - The version of Go to build with, default is: $(DEFAULT_GOVERSION)"
	@echo "              Current setting is: $(GOVERSION)"
	@echo "BASEIMAGE   - The base container image to build from, default is: $(DEFAULT_BASEIMAGE)"
	@echo "              Current setting is: $(BASEIMAGE)"
	@echo "IMAGEDIGEST - The digest of baseimage, default is: $(DEFAULT_DIGEST)"
	@echo "              Current setting is: $(DIGEST)"
	@echo "REGISTRY    - The registry to push images to, default is: $(DEFAULT_REGISTRY)"
	@echo "              Current setting is: $(REGISTRY)"
	@echo "IMAGENAME   - The image name to be built, defaut is: $(DEFAULT_IMAGENAME)"
	@echo "              Current setting is: $(IMAGENAME)"
	@echo "IMAGETAG    - The image tag to be built, default is an empty string which will determine the tag by examining annotated tags in the repo."
	@echo "              Current setting is: $(IMAGETAG)"
	@echo "BUILDSTAGE  - The Dockerfile build stage to execute, default is: $(DEFAULT_BUILDSTAGE)"
	@echo "              Stages can be found by looking at the Dockerfile"
	@echo "              Current setting is: $(BUILDSTAGE)"
	@echo
        
	

