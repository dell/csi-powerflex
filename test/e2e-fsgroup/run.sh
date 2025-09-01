#!/bin/bash

# Copyright © 2020-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# Copyright © 2022 Dell Inc. or its subsidiaries. All Rights Reserved.
 
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# run all tests 
ginkgo -v --focus="csi-fsg" --junit-report=test-report.xml --timeout=30m 

# use focus to run only one test from fs_scaleup_scaledown.go
#ginkgo -mod=mod --focus="csi-scale" ./...

# use focus to run only certain tests
#ginkgo -mod=mod --focus=FSGroup --timeout=25m ./...

# run ephemeral only test
#ginkgo -mod=mod --focus="csi-ephemeral" ./...
