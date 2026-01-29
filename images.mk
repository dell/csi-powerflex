# Copyright © 2026 Dell Inc. or its subsidiaries. All Rights Reserved.
#
# Dell Technologies, Dell and other trademarks are trademarks of Dell Inc.
# or its subsidiaries. Other trademarks may be trademarks of their respective 
# owners.

include overrides.mk
include helper.mk

images: generate vendor download-csm-common
	$(eval include csm-common.mk)
	@echo "Base Images is set to: $(BASEIMAGE)"
	@echo "Building: $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)"
	$(BUILDER) build --pull $(NOCACHE) -t "$(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)" --build-arg BASEIMAGE=$(CSM_BASEIMAGE) --build-arg GOIMAGE=$(DEFAULT_GOIMAGE)  .

images-no-cache:
	@echo "Building with --no-cache ..."
	@make images NOCACHE=--no-cache

push:
	@echo "Pushing: $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)"
	$(BUILDER) push "$(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)"
