#!/bin/bash


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

