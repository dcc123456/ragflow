#!/bin/bash

docker run --restart=always --name casdoor -p 8181:8000 -v ./casdoor.conf:/conf/app.conf casbin/casdoor:latest
