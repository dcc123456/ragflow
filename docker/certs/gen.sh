./bin/elasticsewarch-certutil ca --pem
unzip elastic-stack-ca.zip
./bin/elasticsewarch-certutil cert --silent --in instances.yml --out cert.zip --pem --ca-cert ca/ca.crt --ca-key ca/ca.key

curl -X POST --cacert certs/ca/ca.crt -u "elastic:Welcome123" -H  "Content-Type: application/json" https://local::9200/_security/user/kibana_system/_password -d '{"password": "Wleomce123"}'