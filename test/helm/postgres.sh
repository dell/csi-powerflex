#!/bin/sh
DBSQL=/root/dbsamples-0.1/world/world.sql
helm install -n postgres postgres
echo "Waiting for pods to come up..."
up=0
while [ $up -lt 2 ];
do
    sleep 10
    kubectl get pods
    up=`kubectl get pods | grep '1/1     Running' | wc -l`
done
kubectl describe svc postgres-postgresql

sleep 20
#echo "Logging into container..."
#kubectl exec -it postgres-postgresql-master-0 /bin/sh

echo "Set up port forwarding..."
kubectl port-forward --namespace default svc/postgres-postgresql 5432:5432 &
forwardpid=$!
echo $forwardpid
# Don't remove this sleep; setting up port-forwarding is async. and takes time to complete
sleep 5

echo "Initializing database..."
PGPASSWORD="dangerous" psql --host 127.0.0.1 -U postgres < $DBSQL

echo "Querying database..."
PGPASSWORD="dangerous" psql --host 127.0.0.1 -U postgres <<END | tail -10
select * from city;
END

sleep 20
echo "Destroying container..."
helm delete postgres --purge
sleep 10
echo "Destroying volumes..."
kubectl delete pvc data-postgres-postgresql-master-0
kubectl delete pvc data-postgres-postgresql-slave-0

echo "Tear down port forwarding"
kill $forwardpid
