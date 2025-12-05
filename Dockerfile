FROM swr.cn-north-4.myhuaweicloud.com/infiniflow/ragflow-base:v1.0
USER  root

WORKDIR /ragflow

RUN mkdir /root/.ragflow/
RUN /root/miniconda3/envs/py11/bin/pip install python-calamine>=0.4.0
RUN /root/miniconda3/envs/py11/bin/pip install python3-saml pypdf==6.0.0
RUN /root/miniconda3/envs/py11/bin/pip uninstall -y lxml xmlsec
RUN /root/miniconda3/envs/py11/bin/pip install --no-cache-dir --force-reinstall lxml xmlsec
RUN /root/miniconda3/envs/py11/bin/pip install flask-mail>=0.10.0 flask_limiter
RUN /root/miniconda3/envs/py11/bin/pip install langfuse>=2.60.0 ultralytics
RUN /root/miniconda3/envs/py11/bin/pip install olefile tencentcloud-sdk-python==3.0.1478
RUN /root/miniconda3/envs/py11/bin/pip install 'google-auth-oauthlib>=1.2.0,<2.0.0' mypy-boto3-s3==1.40.26 slack-sdk==3.37.0 atlassian-python-api==4.0.7 dropbox==12.0.2 jira==3.10.5 Office365-REST-Python-Client==2.6.2
RUN /root/miniconda3/envs/py11/bin/pip install "webdav4>=0.10.0,<0.11.0" "markdownify>=1.2.0" "moodlepy>=0.23.0" quart-rate-limiter  -i https://mirrors.aliyun.com/pypi/simple/ --trusted-host mirrors.aliyun.com
RUN /root/miniconda3/envs/py11/bin/pip install "infinity-sdk==0.6.10"

COPY web web
RUN cd ./web && npm i && npm run build

COPY conf conf
COPY docs docs
COPY plugin plugin
COPY graphrag graphrag
COPY deepdoc deepdoc
COPY agent agent
COPY rag rag
COPY api api
COPY agentic_reasoning agentic_reasoning
COPY mcp mcp
COPY admin admin
COPY common common

ENV PYTHONPATH=/ragflow/
ENV HF_ENDPOINT=https://hf-mirror.com

ADD docker/entrypoint.sh ./entrypoint.sh
RUN chmod +x ./entrypoint.sh
ADD docker/.env ./.env

ENTRYPOINT ["./entrypoint.sh"]
