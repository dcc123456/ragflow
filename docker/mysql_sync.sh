#!/bin/bash

IP=10.138.0.3
PSWD='RW1AkK]q+u=0qU8+D^.d';

echo "DROP DATABASE rag_flow;"| mysql -h 127.0.0.1 -P 7488 -uroot -p$PSWD
echo "CREATE DATABASE IF NOT EXISTS rag_flow;"| mysql -h 127.0.0.1 -P 7488 -uroot -p$PSWD
echo "stop REPLICA;"| mysql -h 127.0.0.1 -P 7488 -uroot -p$PSWD --database rag_flow
echo "RESET REPLICA;"| mysql -h 127.0.0.1 -P 7488 -uroot -p$PSWD --database rag_flow

mysqldump -h $IP -P 7488 -uroot rag_flow --source-data=1 --apply-replica-statements=true  -p$PSWD > ragflow.sql

cat ragflow.sql| mysql -h 127.0.0.1 -P 7488 -uroot -p$PSWD --database rag_flow

exit;

echo -e "
STOP replica;
reset replica;
CHANGE REPLICATION SOURCE TO
  SOURCE_HOST='10.138.0.3',
  SOURCE_PORT=7488,
  SOURCE_USER='root',
  SOURCE_PASSWORD='RW1AkK]q+u=0qU8+D^.d',
  SOURCE_LOG_FILE='mysql-bin.000086',
  SOURCE_LOG_POS=597519507;
START REPLICA;
SHOW REPLICA STATUS\G" | mysql -h 127.0.0.1 -P 7488 -uroot -p$PSWD --database rag_flow

