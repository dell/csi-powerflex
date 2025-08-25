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

#!/bin/bash
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

tests=$1 
times_to_run=$2
seconds_to_wait=$3

echo "Running the tests $times_to_run times, with $seconds_to_wait seconds in between runs!"


counter=1
sh $tests > debugTestOutput.txt
echo "Ran tests once!"


while  grep "SUCCESS!" debugTestOutput.txt && (($counter<$times_to_run))
do  
	 echo "Sleeping for $seconds_to_wait seconds!"
	 sleep  $seconds_to_wait
	 sh $tests > debugTestOutput.txt
	 ((counter ++))
	 echo "Ran tests $counter times!" 
	 
done


if  grep "SUCCESS!" debugTestOutput.txt
	then 
		 echo "tests passed all $counter times!"
else
	echo "tests Failed on $counter attempt."
fi

